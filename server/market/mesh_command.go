// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"fmt"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/mesh"
)

const (
	commandKindLimit           = "limit"
	commandKindMarket          = "market"
	commandKindCancel          = "cancel"
	commandKindScheduleSuspend = "schedule_suspend"
	commandKindScheduleResume  = "schedule_resume"

	lifecycleCommandTimeout = 2 * time.Minute
)

// Commands returns the order router's mesh command executors.
func (r *OrderRouter) Commands() map[string]mesh.CommandExecutor {
	return map[string]mesh.CommandExecutor{
		commandKindLimit:  r.executeLimit,
		commandKindMarket: r.executeMarket,
		commandKindCancel: r.executeCancel,
	}
}

type scheduleSuspendRequest struct {
	Market      string `json:"market"`
	TimeMs      int64  `json:"timeMs,omitempty"`
	PersistBook bool   `json:"persistBook"`
}

type scheduleSuspendResult struct {
	EpochIdx int64 `json:"epochIdx"`
	EndMs    int64 `json:"endMs"`
}

type scheduleResumeRequest struct {
	Market string `json:"market"`
	TimeMs int64  `json:"timeMs,omitempty"`
}

type scheduleResumeResult struct {
	EpochIdx int64 `json:"epochIdx"`
	StartMs  int64 `json:"startMs"`
}

// LifecycleCommands returns the command handlers for scheduling market
// suspension and resumption.
func LifecycleCommands(markets map[string]*Market) map[string]mesh.CommandExecutor {
	return map[string]mesh.CommandExecutor{
		commandKindScheduleSuspend: func(cmdCtx *mesh.CommandContext) *msgjson.Error {
			return handleScheduleSuspend(cmdCtx, markets)
		},
		commandKindScheduleResume: func(cmdCtx *mesh.CommandContext) *msgjson.Error {
			return handleScheduleResume(cmdCtx, markets)
		},
	}
}

// ExecuteScheduleSuspend schedules a market suspension and returns the
// scheduled final trading epoch and its end time.
func ExecuteScheduleSuspend(ctx context.Context, meshSvc *mesh.Service, market string, asSoonAs time.Time, persistBook bool) (*SuspendEpoch, error) {
	if meshSvc == nil {
		return nil, fmt.Errorf("mesh service is not configured")
	}
	req := &scheduleSuspendRequest{
		Market:      market,
		PersistBook: persistBook,
	}
	if !asSoonAs.IsZero() {
		req.TimeMs = asSoonAs.UnixMilli()
	}
	var result scheduleSuspendResult
	if err := executeLifecycleCommand(ctx, meshSvc.ExecuteCommand, commandKindScheduleSuspend, req, &result); err != nil {
		return nil, err
	}
	if result.EpochIdx == 0 {
		return nil, fmt.Errorf("schedule_suspend returned zero epoch")
	}
	return &SuspendEpoch{Idx: result.EpochIdx, End: time.UnixMilli(result.EndMs)}, nil
}

// handleScheduleSuspend builds and emits the suspension scheduling event,
// then responds with the final trading epoch.
func handleScheduleSuspend(cmdCtx *mesh.CommandContext, markets map[string]*Market) *msgjson.Error {
	var req scheduleSuspendRequest
	if err := cmdCtx.Request.Msg.Unmarshal(&req); err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error parsing schedule_suspend request")
	}

	mkt := markets[req.Market]
	if mkt == nil {
		return msgjson.NewError(msgjson.UnknownMarketError, "unknown market %s", req.Market)
	}

	event, susp, err := mkt.buildScheduleSuspendEvent(unixMilliOrZero(req.TimeMs), req.PersistBook)
	if err != nil {
		return msgjson.NewError(msgjson.RPCInternalError, "failed to schedule suspend: %v", err)
	}

	if err = cmdCtx.Completion.Emit(cmdCtx.Context, event, func() any {
		return &scheduleSuspendResult{EpochIdx: susp.Idx, EndMs: susp.End.UnixMilli()}
	}); err != nil {
		mesh.LogApplyFailure(log, err, "Failed to apply schedule_suspend for market %s: %v", req.Market, err)
		return mesh.ClientError(err, msgjson.RPCInternalError, "failed to apply schedule_suspend: %v", err)
	}
	return nil
}

// ExecuteScheduleResume schedules a market resumption and returns the
// scheduled starting epoch and its start time.
func ExecuteScheduleResume(ctx context.Context, meshSvc *mesh.Service, market string, asSoonAs time.Time) (startEpoch int64, startTime time.Time, err error) {
	if meshSvc == nil {
		return 0, time.Time{}, fmt.Errorf("mesh service is not configured")
	}
	req := &scheduleResumeRequest{Market: market}
	if !asSoonAs.IsZero() {
		req.TimeMs = asSoonAs.UnixMilli()
	}
	var result scheduleResumeResult
	if err := executeLifecycleCommand(ctx, meshSvc.ExecuteCommand, commandKindScheduleResume, req, &result); err != nil {
		return 0, time.Time{}, err
	}
	if result.EpochIdx == 0 {
		return 0, time.Time{}, fmt.Errorf("schedule_resume returned zero epoch")
	}
	return result.EpochIdx, time.UnixMilli(result.StartMs), nil
}

// handleScheduleResume builds and emits the resumption scheduling event,
// then responds with the scheduled starting epoch.
func handleScheduleResume(cmdCtx *mesh.CommandContext, markets map[string]*Market) *msgjson.Error {
	var req scheduleResumeRequest
	if err := cmdCtx.Request.Msg.Unmarshal(&req); err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error parsing schedule_resume request")
	}

	mkt := markets[req.Market]
	if mkt == nil {
		return msgjson.NewError(msgjson.UnknownMarketError, "unknown market %s", req.Market)
	}

	event, startEpoch, startTime, err := mkt.buildScheduleResumeEvent(unixMilliOrZero(req.TimeMs))
	if err != nil {
		return msgjson.NewError(msgjson.RPCInternalError, "failed to schedule resume: %v", err)
	}

	if err = cmdCtx.Completion.Emit(cmdCtx.Context, event, func() any {
		return &scheduleResumeResult{EpochIdx: startEpoch, StartMs: startTime.UnixMilli()}
	}); err != nil {
		mesh.LogApplyFailure(log, err, "Failed to apply schedule_resume for market %s: %v", req.Market, err)
		return mesh.ClientError(err, msgjson.RPCInternalError, "failed to apply schedule_resume: %v", err)
	}
	return nil
}

// executeLifecycleCommand executes a lifecycle command and decodes its
// response into result.
func executeLifecycleCommand(ctx context.Context, execute func(context.Context, mesh.CommandRequest) *msgjson.Error, kind string, payload, result any) error {
	cmdCtx, cancel := context.WithTimeout(ctx, lifecycleCommandTimeout)
	defer cancel()

	msg, err := msgjson.NewRequest(comms.NextID(), kind, payload)
	if err != nil {
		return err
	}

	responses := make(chan *msgjson.Message, 1)
	execErrs := make(chan *msgjson.Error, 1)
	go func() {
		if err := execute(cmdCtx, mesh.CommandRequest{
			Kind: kind,
			Msg:  msg,
			Respond: func(resp *msgjson.Message) error {
				select {
				case responses <- resp:
				default:
				}
				return nil
			},
		}); err != nil {
			execErrs <- err
		}
	}()

	decodeResponse := func(resp *msgjson.Message) error {
		if resp == nil {
			return fmt.Errorf("nil %s response", kind)
		}
		return resp.UnmarshalResult(result)
	}

	select {
	case rpcErr := <-execErrs:
		return rpcErr
	case resp := <-responses:
		return decodeResponse(resp)
	case <-cmdCtx.Done():
		select {
		case resp := <-responses:
			return decodeResponse(resp)
		default:
			return cmdCtx.Err()
		}
	}

}

// unixMilliOrZero converts positive Unix milliseconds to a time.
// Nonpositive values return the zero time.
func unixMilliOrZero(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}
