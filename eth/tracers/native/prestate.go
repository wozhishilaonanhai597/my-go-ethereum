// Copyright 2022 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package native

import (
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/internal"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

//go:generate go run github.com/fjl/gencodec -type account -field-override accountMarshaling -out gen_account_json.go

func init() {
	tracers.DefaultDirectory.Register("prestateTracer", newPrestateTracer, false)
}

type event struct {
	Caller  common.Address `json:"caller,omitempty"`
	Topics0 uint256.Int    `json:"topics0,omitempty"`
	Data    []string       `json:"data,omitempty"`
}

type prestateTracer struct {
	noopTracer
	pre    []event
	isFail bool
	reason error // Textual reason for the interruption
}

type prestateTracerConfig struct {
	DiffMode bool `json:"diffMode"` // If true, this tracer will return state modifications
}

func newPrestateTracer(ctx *tracers.Context, cfg json.RawMessage, chainConfig *params.ChainConfig) (*tracers.Tracer, error) {
	var config prestateTracerConfig
	if err := json.Unmarshal(cfg, &config); err != nil {
		return nil, err
	}
	t := &prestateTracer{
		pre: []event{},
	}
	return &tracers.Tracer{
		Hooks: &tracing.Hooks{
			OnTxStart: t.OnTxStart,
			OnTxEnd:   t.OnTxEnd,
			OnOpcode:  t.OnOpcode,
		},
		GetResult: t.GetResult,
		Stop:      t.Stop,
	}, nil
}

// OnOpcode implements the EVMLogger interface to trace a single step of VM execution.
func (t *prestateTracer) OnOpcode(pc uint64, opcode byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
	if opcode == 0xfd {
		t.isFail = true
		return
	}
	if opcode > 0xa0 && opcode <= 0xa4 {
		stackData := scope.StackData()
		stackLen := len(stackData)

		caller := scope.Address()
		offset := stackData[stackLen-1]
		size := stackData[stackLen-2]
		topics0 := stackData[stackLen-3]

		data, err := internal.GetMemoryCopyPadded(scope.MemoryData(), int64(offset.Uint64()), int64(size.Uint64()))
		if err != nil {
			log.Warn("failed to copy CREATE2 input", "err", err, "tracer", "prestateTracer", "offset", offset, "size", size)
			return
		}
		var dataRes []string
		for i := 0; i < len(data); i += 32 {
			end := i + 32
			if end > len(data) {
				end = len(data)
			}
			slice := data[i:end]
			hexString := "0x" + hex.EncodeToString(slice)
			dataRes = append(dataRes, hexString)
		}
		t.lookupLog(caller, dataRes, topics0)
	}
}

func (t *prestateTracer) OnTxStart(env *tracing.VMContext, tx *types.Transaction, from common.Address) {
}

func (t *prestateTracer) OnTxEnd(receipt *types.Receipt, err error) {
}

// GetResult returns the json-encoded nested list of call traces, and any
// error arising from the encoding or forceful termination (via `Stop`).
func (t *prestateTracer) GetResult() (json.RawMessage, error) {
	var res []byte
	var err error
	res, err = json.Marshal(struct {
		Event  []event `json:"event"`
		IsFail bool    `json:"isFail"`
		Reason error   `json:"reason"`
	}{t.pre, t.isFail, t.reason})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(res), t.reason
}

// Stop terminates execution of the tracer at the first opportune moment.
func (t *prestateTracer) Stop(err error) {
	t.reason = err
	t.isFail = true
}

func (t *prestateTracer) lookupLog(addr common.Address, data []string, topics0 uint256.Int) {
	t.pre = append(t.pre, event{addr, topics0, data})
}
