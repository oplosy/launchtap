package chain

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

var (
	ErrMalformedLog        = errors.New("malformed chain log")
	ErrUnknownTopic        = errors.New("unknown event topic")
	ErrUnknownEmitter      = errors.New("unknown event emitter")
	ErrUnsupportedEngine   = errors.New("unsupported engine version")
	ErrFinalityUnsupported = errors.New("provider does not support finality tag")
	ErrRPCCapacity         = errors.New("RPC log query exceeds provider capacity")
	ErrChainIDMismatch     = errors.New("RPC chain ID does not match configured deployment")
	ErrBytecodeMismatch    = errors.New("runtime bytecode hash mismatch")
	ErrPairMismatch        = errors.New("pair address mismatch")
	ErrLaunchMismatch      = errors.New("launch does not match the deployment manifest")
	ErrPoolMismatch        = errors.New("V4 pool does not match the launch pool key")
)

type ChainIDMismatchError struct {
	ConfiguredChainID uint64
	DeploymentChainID uint64
	ActualChainID     string
}

func (e *ChainIDMismatchError) Error() string {
	return fmt.Sprintf("%v: configured=%d deployment=%d RPC=%s", ErrChainIDMismatch, e.ConfiguredChainID, e.DeploymentChainID, e.ActualChainID)
}

func (e *ChainIDMismatchError) Unwrap() error { return ErrChainIDMismatch }

type LogError struct {
	Coordinates LogCoordinates
	Err         error
}

func (e *LogError) Error() string { return fmt.Sprintf("decode log (%s): %v", e.Coordinates, e.Err) }
func (e *LogError) Unwrap() error { return e.Err }

type EmitterError struct {
	Address     common.Address
	Coordinates LogCoordinates
}

func (e *EmitterError) Error() string {
	return fmt.Sprintf("%v %s at %s", ErrUnknownEmitter, e.Address.Hex(), e.Coordinates)
}
func (e *EmitterError) Unwrap() error { return ErrUnknownEmitter }

type EngineVersionError struct{ Version uint16 }

func (e *EngineVersionError) Error() string {
	return fmt.Sprintf("%v: %d", ErrUnsupportedEngine, e.Version)
}
func (e *EngineVersionError) Unwrap() error { return ErrUnsupportedEngine }

type FinalityTagError struct {
	Tag string
	Err error
}

func (e *FinalityTagError) Error() string {
	return fmt.Sprintf("read %s head: %v: %v", e.Tag, ErrFinalityUnsupported, e.Err)
}
func (e *FinalityTagError) Unwrap() error { return ErrFinalityUnsupported }
