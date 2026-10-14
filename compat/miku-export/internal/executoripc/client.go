package executoripc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"reflect"
	"runtime"
	"time"

	"mikugate/internal/netexec"
)

type Client struct {
	instance string
	dial     func(context.Context, string) (net.Conn, error)
}

func NewClient(instance string) (*Client, error) {
	if _, err := SocketPath(instance); err != nil {
		return nil, err
	}
	if runtime.GOOS != "linux" {
		return nil, ErrUnsupported
	}
	return &Client{instance: instance, dial: dialTrusted}, nil
}
func (c *Client) call(ctx context.Context, operation, exit string, apply bool) (response, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return response{}, err
	}
	request := Request{Version: Version, ID: hex.EncodeToString(nonce[:]), Operation: operation, ExitID: exit, Apply: apply}
	if err := request.validate(); err != nil {
		return response{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	conn, err := c.dial(ctx, c.instance)
	if err != nil {
		return response{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return response{}, ErrUnavailable
	}
	if err = conn.SetWriteDeadline(time.Now().Add(IOTimeout)); err != nil {
		return response{}, ErrUnavailable
	}
	if err = writeFrame(conn, request, MaxRequestBytes); err != nil {
		return response{}, ErrUnavailable
	}
	data, err := readFrame(conn, MaxResponseBytes)
	if err != nil {
		if ctx.Err() != nil {
			return response{}, ctx.Err()
		}
		return response{}, ErrUnavailable
	}
	var result response
	if decode(data, &result) != nil || result.Version != Version || result.ID != request.ID {
		return response{}, ErrProtocol
	}
	if result.Error != nil {
		expected := rpcError(result.Error.Code)
		if expected == nil || !reflect.DeepEqual(expected, result.Error) || result.Result != nil || result.Capabilities != nil {
			return response{}, ErrProtocol
		}
		return response{}, result.Error
	}
	if operation == "capabilities" {
		expected := capabilities(c.instance)
		if result.Result != nil || result.Capabilities == nil || !reflect.DeepEqual(*result.Capabilities, expected) {
			return response{}, ErrProtocol
		}
	} else if result.Capabilities != nil || result.Result == nil || !validResult(c.instance, exit, *result.Result) {
		return response{}, ErrProtocol
	}
	return result, nil
}
func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	r, err := c.call(ctx, "capabilities", "", false)
	if err != nil {
		return Capabilities{}, err
	}
	return *r.Capabilities, nil
}
func (c *Client) Inspect(ctx context.Context, exit string) (netexec.Result, error) {
	r, err := c.call(ctx, "inspect", exit, false)
	if err != nil {
		return netexec.Result{}, err
	}
	return *r.Result, nil
}
func (c *Client) Prepare(ctx context.Context, exit string) (netexec.Result, error) {
	r, err := c.call(ctx, "prepare", exit, true)
	if err != nil {
		return netexec.Result{}, err
	}
	return *r.Result, nil
}
func (c *Client) Cleanup(ctx context.Context, exit string) (netexec.Result, error) {
	r, err := c.call(ctx, "cleanup", exit, true)
	if err != nil {
		return netexec.Result{}, err
	}
	return *r.Result, nil
}
