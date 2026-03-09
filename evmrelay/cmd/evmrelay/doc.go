// Package main implements the evmrelay command.
//
// The relay accepts EIP-712 signed redeem requests and executes them from a
// dedicated relayer account. The task lifecycle is intentionally queue-based
// so nonce handling remains simple and crash recovery is durable.
//
// Lifecycle overview:
//
//  1. Ingress
//     POST /api/relay validates the target, calldata shape, signed fee
//     recipient, deadline, and participant nonce. The relay reserves the
//     participant+contract nonce slot before accepting the request so retries
//     are idempotent and conflicting submissions are rejected.
//
//  2. Queueing
//     Accepted requests are persisted as queued tasks with public state
//     "pending". At this point no relayer transaction nonce has been assigned,
//     so queued tasks can still fail or expire without creating a nonce gap for
//     later work. A queued task is eligible until ValidUntil, which is the
//     earlier of:
//     - the signed deadline minus a 2 minute safety margin
//     - 30 minutes after submission
//
//  3. Promotion
//     Each configured chain has a single background worker. When a chain has no
//     active nonce-owning task, the worker promotes the next queued task,
//     revalidates it against current chain conditions, assigns the relayer
//     account nonce, signs the redeem transaction, persists the raw tx, and
//     broadcasts it. The worker runs every 5 seconds and each pass is bounded
//     by a 30 second timeout so a stuck RPC call cannot block that chain
//     forever.
//
//  4. Active processing
//     An active task stays publicly "pending" while the worker checks receipts
//     for every same-nonce redeem candidate hash. If a redeem tx is stuck, the
//     relay can replace it with a higher-fee same-nonce redeem tx. Any redeem
//     candidate can still win the race and become the canonical mined result.
//     The retry policy is:
//     - rebroadcast the current raw tx every 30 seconds while it remains
//     unresolved
//     - attempt a redeem replacement every 2 minutes while the task is still
//     before ValidUntil
//     - allow at most 3 redeem replacements, so a task can have at most
//     4 redeem tx hashes total (1 initial + 3 replacements)
//     Each replacement keeps the same nonce and bumps fees to at least the
//     larger of:
//     - the current network suggestion
//     - 110% of the previous tx's fee cap and tip cap
//     If the replacement limit is reached, the relay stops fee-bumping the
//     redeem and just keeps rebroadcasting the latest redeem tx until the task
//     expires or one candidate is mined.
//
//  5. Cancellation
//     Once a task passes ValidUntil, the relay submits a same-nonce cancel/noop
//     tx and moves the task to the internal canceling phase. The task remains
//     publicly "pending" until one same-nonce candidate is actually mined. If
//     the cancel gets stuck, it can also be replaced with a higher-fee
//     same-nonce cancel tx. The cancellation policy is:
//     - submit the first cancel tx immediately after the task expires
//     - rebroadcast the current cancel tx every 30 seconds while unresolved
//     - attempt a cancel replacement every 2 minutes
//     - allow at most 3 cancel replacements, so a task can have at most
//     4 cancel tx hashes total (1 initial + 3 replacements)
//     Cancel replacements use the same 110% minimum fee bump rule as redeem
//     replacements. If the cancel replacement limit is reached, the relay keeps
//     rebroadcasting the latest cancel tx until either a redeem candidate or a
//     cancel candidate is mined.
//
//  6. Finalization
//     A task becomes "success" only when a redeem candidate is mined
//     successfully. A task becomes "failed" when a redeem candidate reverts, a
//     cancel tx is mined, or the task expires/fails before ever consuming a
//     relayer nonce. Terminal tasks release the participant nonce reservation
//     and are pruned after the retention period.
//
// Persistence is handled by taskStore in task_store.go. Queue promotion and
// active/cancel reconciliation live in tasks.go. The HTTP ingress and status
// endpoints live in api.go.
package main
