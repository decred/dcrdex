package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	unknownCmdErr = errors.New("unknown command")
	noCmdErr      = errors.New("no command")
)

type command int

const (
	quitCmd command = iota
	echoCmd
)

type response struct {
	msg string
	err error
}

func parseString(cmdstr string) (command, []string, error) {
	args := strings.Split(cmdstr, " ")
	var cmd command

	if len(args) == 0 {
		return 0, nil, fmt.Errorf("%v", noCmdErr)
	}

	switch args[0] {
	case "exit", "quit", "goodbye":
		cmd = quitCmd
	case "echo", "say", "quote":
		cmd = echoCmd
	default:
		return 0, nil, fmt.Errorf("%v: %v", unknownCmdErr, cmdstr)
	}

	return cmd, args[1:], nil
}

func cmdListener(ctx context.Context, wg *sync.WaitGroup, cancel context.CancelFunc) (chan<- string, <-chan *response) {
	wg.Add(1)
	defer wg.Done()
	in := make(chan string)
	out := make(chan *response)
	go func() {
	out:
		for {
			select {
			case <-ctx.Done():
				break out
			case cmdstr := <-in:
				cmd, args, err := parseString(cmdstr)
				if err != nil {
					out <- &response{"", err}
					break
				}
				switch cmd {
				case quitCmd:
					cancel()
					break out
				case echoCmd:
					echo(args, out)
				default:
					panic(unknownCmdErr)
				}
			}
		}
		out <- &response{"exiting...", nil}
		time.Sleep(1)
	}()
	return in, out
}

func echo(args []string, ch chan *response) {
	res := &response{}
	for _, a := range args {
		res.msg += a + " "
	}
	ch <- res
}
