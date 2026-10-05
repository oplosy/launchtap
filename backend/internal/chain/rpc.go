package chain

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type RPCConfig struct {
	Timeout      time.Duration
	MaxRetries   uint64
	RetryBackoff time.Duration
}

type Client struct {
	rpc    *rpc.Client
	eth    *ethclient.Client
	config RPCConfig
}

func Dial(ctx context.Context, rawURL string, config RPCConfig) (*Client, error) {
	if config.Timeout <= 0 || config.RetryBackoff <= 0 {
		return nil, errors.New("RPC timeout and retry backoff must be positive")
	}
	httpClient := &http.Client{Timeout: config.Timeout}
	rpcClient, err := rpc.DialOptions(ctx, rawURL, rpc.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("dial chain RPC: %w", err)
	}
	return &Client{rpc: rpcClient, eth: ethclient.NewClient(rpcClient), config: config}, nil
}

func (c *Client) Close() {
	if c != nil && c.rpc != nil {
		c.rpc.Close()
	}
}

func (c *Client) HeaderByNumber(ctx context.Context, number uint64) (*types.Header, error) {
	return c.header(ctx, new(big.Int).SetUint64(number))
}

// HeadersByNumbers fetches the requested headers in one JSON-RPC batch. A missing block or a
// failed batch element fails the whole call, so callers never see a partial range.
func (c *Client) HeadersByNumbers(ctx context.Context, numbers []uint64) ([]*types.Header, error) {
	headers := make([]*types.Header, len(numbers))
	err := c.retry(ctx, false, func(callContext context.Context) error {
		batch := make([]rpc.BatchElem, len(numbers))
		for index, number := range numbers {
			headers[index] = nil
			batch[index] = rpc.BatchElem{Method: "eth_getBlockByNumber", Args: []any{hexutil.EncodeUint64(number), false}, Result: &headers[index]}
		}
		if err := c.rpc.BatchCallContext(callContext, batch); err != nil {
			return err
		}
		for index, element := range batch {
			if element.Error != nil {
				return element.Error
			}
			if headers[index] == nil {
				return fmt.Errorf("header %d: %w", numbers[index], ethereum.NotFound)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return headers, nil
}

func (c *Client) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	var result *types.Header
	err := c.retry(ctx, false, func(callContext context.Context) error {
		var callErr error
		result, callErr = c.eth.HeaderByHash(callContext, hash)
		return callErr
	})
	return result, err
}

type Heads struct{ Latest, Safe, Finalized *types.Header }

func (c *Client) Heads(ctx context.Context) (Heads, error) {
	latest, err := c.header(ctx, big.NewInt(int64(rpc.LatestBlockNumber)))
	if err != nil {
		return Heads{}, fmt.Errorf("read latest head: %w", err)
	}
	safe, err := c.finalityHeader(ctx, "safe", rpc.SafeBlockNumber)
	if err != nil {
		return Heads{}, err
	}
	finalized, err := c.finalityHeader(ctx, "finalized", rpc.FinalizedBlockNumber)
	if err != nil {
		return Heads{}, err
	}
	return Heads{Latest: latest, Safe: safe, Finalized: finalized}, nil
}

func (c *Client) finalityHeader(ctx context.Context, tag string, number rpc.BlockNumber) (*types.Header, error) {
	header, err := c.header(ctx, big.NewInt(int64(number)))
	if err != nil {
		if isUnsupportedTag(err) {
			return nil, &FinalityTagError{Tag: tag, Err: err}
		}
		return nil, fmt.Errorf("read %s head: %w", tag, err)
	}
	return header, nil
}

func isUnsupportedTag(err error) bool {
	if errors.Is(err, ethereum.NotFound) {
		return true
	}
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) && (rpcErr.ErrorCode() == -32601 || rpcErr.ErrorCode() == -32602) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unsupported") || strings.Contains(message, "unknown block") || strings.Contains(message, "invalid argument")
}

func (c *Client) header(ctx context.Context, number *big.Int) (*types.Header, error) {
	var result *types.Header
	err := c.retry(ctx, false, func(callContext context.Context) error {
		var callErr error
		result, callErr = c.eth.HeaderByNumber(callContext, number)
		return callErr
	})
	return result, err
}

func (c *Client) CodeAt(ctx context.Context, address common.Address) ([]byte, error) {
	var result []byte
	err := c.retry(ctx, false, func(callContext context.Context) error {
		var callErr error
		result, callErr = c.eth.CodeAt(callContext, address, nil)
		return callErr
	})
	return result, err
}

func (c *Client) ChainID(ctx context.Context) (*big.Int, error) {
	var result *big.Int
	err := c.retry(ctx, false, func(callContext context.Context) error {
		var callErr error
		result, callErr = c.eth.ChainID(callContext)
		return callErr
	})
	return result, err
}

func (c *Client) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	var result []types.Log
	err := c.retry(ctx, true, func(callContext context.Context) error {
		var callErr error
		result, callErr = c.eth.FilterLogs(callContext, query)
		return callErr
	})
	return result, err
}

func (c *Client) CallContract(ctx context.Context, message ethereum.CallMsg) ([]byte, error) {
	var result []byte
	err := c.retry(ctx, false, func(callContext context.Context) error {
		var callErr error
		result, callErr = c.eth.CallContract(callContext, message, nil)
		return callErr
	})
	return result, err
}

func (c *Client) retry(ctx context.Context, capacitySensitive bool, operation func(context.Context) error) error {
	var err error
	for attempt := uint64(0); attempt <= c.config.MaxRetries; attempt++ {
		callContext, cancel := context.WithTimeout(ctx, c.config.Timeout)
		err = operation(callContext)
		cancel()
		if err == nil || (capacitySensitive && IsCapacityError(err)) || !isRetryable(err) {
			return err
		}
		if attempt == c.config.MaxRetries {
			break
		}
		timer := time.NewTimer(c.config.RetryBackoff * time.Duration(attempt+1))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func isRetryable(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return true
	}
	var httpErr rpc.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == http.StatusTooManyRequests || httpErr.StatusCode >= 500
	}
	var rpcErr rpc.Error
	return errors.As(err, &rpcErr) && rpcErr.ErrorCode() <= -32000 && rpcErr.ErrorCode() >= -32099
}

func IsCapacityError(err error) bool {
	if err == nil {
		return false
	}
	var httpErr rpc.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusRequestEntityTooLarge {
		return true
	}
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) && (rpcErr.ErrorCode() == -32003 || rpcErr.ErrorCode() == -32005) {
		return true
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "rate limit") || strings.Contains(message, "too many requests") {
		return false
	}
	patterns := []string{"response too large", "query returned more than", "logs matched by query exceeds limit", "block range too large", "too many results"}
	for _, pattern := range patterns {
		if strings.Contains(message, pattern) {
			return true
		}
	}
	return false
}
