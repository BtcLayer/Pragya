package testutil

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/ethereum-optimism/optimism/cannon/mipsevm"
)

func CopyRegisters(state mipsevm.FPVMState) *[32]uint32 {
	copy := new([32]uint32)
	*copy = *state.GetRegistersRef()
	return copy
}

type StateMutator interface {
	SetPreimageKey(val common.Hash)
	SetPreimageOffset(val uint32)
	SetPC(val uint32)
	SetNextPC(val uint32)
	SetHI(val uint32)
	SetLO(val uint32)
	SetHeap(addr uint32)
	SetExitCode(val uint8)
	SetExited(val bool)
	SetStep(val uint64)
	SetLastHint(val hexutil.Bytes)
	GetRegistersRef() *[32]uint32
}

type StateOption func(state StateMutator)

func WithPC(pc uint32) StateOption {
	return func(state StateMutator) {
		state.SetPC(pc)
	}
}

func WithNextPC(nextPC uint32) StateOption {
	return func(state StateMutator) {
		state.SetNextPC(nextPC)
	}
}

func WithHeap(addr uint32) StateOption {
	return func(state StateMutator) {
		state.SetHeap(addr)
	}
}

func WithLastHint(lastHint hexutil.Bytes) StateOption {
	return func(state StateMutator) {
		state.SetLastHint(lastHint)
	}
}

func WithPreimageKey(key common.Hash) StateOption {
	return func(state StateMutator) {
		state.SetPreimageKey(key)
	}
}

func WithPreimageOffset(offset uint32) StateOption {
	return func(state StateMutator) {
		state.SetPreimageOffset(offset)
	}
}

func WithStep(step uint64) StateOption {
	return func(state StateMutator) {
		state.SetStep(step)
	}
}

func WithRandomization(seed int64) StateOption {
	return func(mut StateMutator) {
		RandomizeState(seed, mut)
	}
}

func RandomizeState(seed int64, mut StateMutator) {
	r := rand.New(rand.NewSource(seed))

	// Memory-align random pc and leave room for nextPC
	pc := r.Uint32() & 0xFF_FF_FF_FC // Align address
	if pc >= 0xFF_FF_FF_FC {
		// Leave room to set and then increment nextPC
		pc = 0xFF_FF_FF_FC - 8
	}

	// Set random step, but leave room to increment
	step := r.Uint64()
	if step == ^uint64(0) {
		step -= 1
	}

	mut.SetPreimageKey(randHash(r))
	mut.SetPreimageOffset(r.Uint32())
	mut.SetPC(pc)
	mut.SetNextPC(pc + 4)
	mut.SetHI(r.Uint32())
	mut.SetLO(r.Uint32())
	mut.SetHeap(r.Uint32())
	mut.SetStep(step)
	mut.SetLastHint(randHint(r))
	*mut.GetRegistersRef() = *randRegisters(r)
}

type ExpectedState struct {
	PreimageKey    common.Hash
	PreimageOffset uint32
	PC             uint32
	NextPC         uint32
	HI             uint32
	LO             uint32
	Heap           uint32
	ExitCode       uint8
	Exited         bool
	Step           uint64
	LastHint       hexutil.Bytes
	Registers      [32]uint32
	MemoryRoot     common.Hash
}

func CreateExpectedState(fromState mipsevm.FPVMState) *ExpectedState {
	return &ExpectedState{
		PreimageKey:    fromState.GetPreimageKey(),
		PreimageOffset: fromState.GetPreimageOffset(),
		PC:             fromState.GetPC(),
		NextPC:         fromState.GetCpu().NextPC,
		HI:             fromState.GetCpu().HI,
		LO:             fromState.GetCpu().LO,
		Heap:           fromState.GetHeap(),
		ExitCode:       fromState.GetExitCode(),
		Exited:         fromState.GetExited(),
		Step:           fromState.GetStep(),
		LastHint:       fromState.GetLastHint(),
		Registers:      *fromState.GetRegistersRef(),
		MemoryRoot:     fromState.GetMemory().MerkleRoot(),
	}
}

type StateValidationFlags int

// TODO(cp-983) - Remove these validation hacks
const (
	SkipMemoryValidation StateValidationFlags = iota
	SkipHintValidation
	SkipPreimageKeyValidation
)

func (e *ExpectedState) Validate(t testing.TB, actualState mipsevm.FPVMState, flags ...StateValidationFlags) {
	if !slices.Contains(flags, SkipPreimageKeyValidation) {
		require.Equal(t, e.PreimageKey, actualState.GetPreimageKey(), fmt.Sprintf("Expect preimageKey = %v", e.PreimageKey))
	}
	require.Equal(t, e.PreimageOffset, actualState.GetPreimageOffset(), fmt.Sprintf("Expect preimageOffset = %v", e.PreimageOffset))
	require.Equal(t, e.PC, actualState.GetCpu().PC, fmt.Sprintf("Expect PC = 0x%x", e.PC))
	require.Equal(t, e.NextPC, actualState.GetCpu().NextPC, fmt.Sprintf("Expect nextPC = 0x%x", e.NextPC))
	require.Equal(t, e.HI, actualState.GetCpu().HI, fmt.Sprintf("Expect HI = 0x%x", e.HI))
	require.Equal(t, e.LO, actualState.GetCpu().LO, fmt.Sprintf("Expect LO = 0x%x", e.LO))
	require.Equal(t, e.Heap, actualState.GetHeap(), fmt.Sprintf("Expect heap = 0x%x", e.Heap))
	require.Equal(t, e.ExitCode, actualState.GetExitCode(), fmt.Sprintf("Expect exitCode = 0x%x", e.ExitCode))
	require.Equal(t, e.Exited, actualState.GetExited(), fmt.Sprintf("Expect exited = %v", e.Exited))
	require.Equal(t, e.Step, actualState.GetStep(), fmt.Sprintf("Expect step = %d", e.Step))
	if !slices.Contains(flags, SkipHintValidation) {
		require.Equal(t, e.LastHint, actualState.GetLastHint(), fmt.Sprintf("Expect lastHint = %v", e.LastHint))
	}
	require.Equal(t, e.Registers, *actualState.GetRegistersRef(), fmt.Sprintf("Expect registers = %v", e.Registers))
	if !slices.Contains(flags, SkipMemoryValidation) {
		require.Equal(t, e.MemoryRoot, common.Hash(actualState.GetMemory().MerkleRoot()), fmt.Sprintf("Expect memory root = %v", e.MemoryRoot))
	}
}

func randHash(r *rand.Rand) common.Hash {
	var bytes [32]byte
	_, err := r.Read(bytes[:])
	if err != nil {
		panic(err)
	}
	return bytes
}

func randHint(r *rand.Rand) []byte {
	count := r.Intn(10)

	bytes := make([]byte, count)
	_, err := r.Read(bytes[:])
	if err != nil {
		panic(err)
	}
	return bytes
}

func randRegisters(r *rand.Rand) *[32]uint32 {
	registers := new([32]uint32)
	for i := 0; i < 32; i++ {
		registers[i] = r.Uint32()
	}
	return registers
}

func RandomBytes(t require.TestingT, seed int64, length uint32) []byte {
	r := rand.New(rand.NewSource(seed))
	randBytes := make([]byte, length)
	if _, err := r.Read(randBytes); err != nil {
		require.NoError(t, err)
	}
	return randBytes
}
