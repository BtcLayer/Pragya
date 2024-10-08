package tests

import (
	"bytes"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ethereum-optimism/optimism/cannon/mipsevm/exec"
	"github.com/ethereum-optimism/optimism/cannon/mipsevm/memory"
	"github.com/ethereum-optimism/optimism/cannon/mipsevm/program"
	"github.com/ethereum-optimism/optimism/cannon/mipsevm/testutil"
	preimage "github.com/ethereum-optimism/optimism/op-preimage"
)

const syscallInsn = uint32(0x00_00_00_0c)

func FuzzStateSyscallBrk(f *testing.F) {
	versions := GetMipsVersionTestCases(f)
	f.Fuzz(func(t *testing.T, seed int64) {
		for _, v := range versions {
			t.Run(v.Name, func(t *testing.T) {
				goVm := v.VMFactory(nil, os.Stdout, os.Stderr, testutil.CreateLogger(), testutil.WithRandomization(seed))
				state := goVm.GetState()
				state.GetRegistersRef()[2] = exec.SysBrk
				state.GetMemory().SetMemory(state.GetPC(), syscallInsn)
				step := state.GetStep()

				expected := testutil.CreateExpectedState(state)
				expected.Step += 1
				expected.PC = state.GetCpu().NextPC
				expected.NextPC = state.GetCpu().NextPC + 4
				expected.Registers[2] = program.PROGRAM_BREAK // Return fixed BRK value
				expected.Registers[7] = 0                     // No error

				stepWitness, err := goVm.Step(true)
				require.NoError(t, err)
				require.False(t, stepWitness.HasPreimage())

				expected.Validate(t, state)

				evm := testutil.NewMIPSEVM(v.Contracts)
				evmPost := evm.Step(t, stepWitness, step, v.StateHashFn)
				goPost, _ := goVm.GetState().EncodeWitness()
				require.Equal(t, hexutil.Bytes(goPost).String(), hexutil.Bytes(evmPost).String(),
					"mipsevm produced different state than EVM")
			})
		}
	})
}

func FuzzStateSyscallMmap(f *testing.F) {
	// Add special cases for large memory allocation
	f.Add(uint32(0), uint32(0x1000), uint32(program.HEAP_END), int64(1))
	f.Add(uint32(0), uint32(1<<31), uint32(program.HEAP_START), int64(2))
	// Check edge case - just within bounds
	f.Add(uint32(0), uint32(0x1000), uint32(program.HEAP_END-4096), int64(3))

	versions := GetMipsVersionTestCases(f)
	f.Fuzz(func(t *testing.T, addr uint32, siz uint32, heap uint32, seed int64) {
		for _, v := range versions {
			t.Run(v.Name, func(t *testing.T) {
				goVm := v.VMFactory(nil, os.Stdout, os.Stderr, testutil.CreateLogger(),
					testutil.WithRandomization(seed), testutil.WithHeap(heap))
				state := goVm.GetState()
				step := state.GetStep()

				state.GetRegistersRef()[2] = exec.SysMmap
				state.GetRegistersRef()[4] = addr
				state.GetRegistersRef()[5] = siz
				state.GetMemory().SetMemory(state.GetPC(), syscallInsn)

				expected := testutil.CreateExpectedState(state)
				expected.Step += 1
				expected.PC = state.GetCpu().NextPC
				expected.NextPC = state.GetCpu().NextPC + 4
				if addr == 0 {
					sizAlign := siz
					if sizAlign&memory.PageAddrMask != 0 { // adjust size to align with page size
						sizAlign = siz + memory.PageSize - (siz & memory.PageAddrMask)
					}
					newHeap := heap + sizAlign
					if newHeap > program.HEAP_END || newHeap < heap || sizAlign < siz {
						expected.Registers[2] = exec.SysErrorSignal
						expected.Registers[7] = exec.MipsEINVAL
					} else {
						expected.Heap = heap + sizAlign
						expected.Registers[2] = heap
						expected.Registers[7] = 0 // no error
					}
				} else {
					expected.Registers[2] = addr
					expected.Registers[7] = 0 // no error
				}

				stepWitness, err := goVm.Step(true)
				require.NoError(t, err)
				require.False(t, stepWitness.HasPreimage())

				expected.Validate(t, state)

				evm := testutil.NewMIPSEVM(v.Contracts)
				evmPost := evm.Step(t, stepWitness, step, v.StateHashFn)
				goPost, _ := goVm.GetState().EncodeWitness()
				require.Equal(t, hexutil.Bytes(goPost).String(), hexutil.Bytes(evmPost).String(),
					"mipsevm produced different state than EVM")
			})
		}
	})
}

func FuzzStateSyscallExitGroup(f *testing.F) {
	versions := GetMipsVersionTestCases(f)
	f.Fuzz(func(t *testing.T, exitCode uint8, seed int64) {
		for _, v := range versions {
			t.Run(v.Name, func(t *testing.T) {
				goVm := v.VMFactory(nil, os.Stdout, os.Stderr, testutil.CreateLogger(),
					testutil.WithRandomization(seed))
				state := goVm.GetState()
				state.GetRegistersRef()[2] = exec.SysExitGroup
				state.GetRegistersRef()[4] = uint32(exitCode)
				state.GetMemory().SetMemory(state.GetPC(), syscallInsn)
				step := state.GetStep()

				expected := testutil.CreateExpectedState(state)
				expected.Step += 1
				expected.Exited = true
				expected.ExitCode = exitCode

				stepWitness, err := goVm.Step(true)
				require.NoError(t, err)
				require.False(t, stepWitness.HasPreimage())

				expected.Validate(t, state)

				evm := testutil.NewMIPSEVM(v.Contracts)
				evmPost := evm.Step(t, stepWitness, step, v.StateHashFn)
				goPost, _ := goVm.GetState().EncodeWitness()
				require.Equal(t, hexutil.Bytes(goPost).String(), hexutil.Bytes(evmPost).String(),
					"mipsevm produced different state than EVM")
			})
		}
	})
}

func FuzzStateSyscallFcntl(f *testing.F) {
	versions := GetMipsVersionTestCases(f)
	f.Fuzz(func(t *testing.T, fd uint32, cmd uint32, seed int64) {
		for _, v := range versions {
			t.Run(v.Name, func(t *testing.T) {
				goVm := v.VMFactory(nil, os.Stdout, os.Stderr, testutil.CreateLogger(),
					testutil.WithRandomization(seed))
				state := goVm.GetState()
				state.GetRegistersRef()[2] = exec.SysFcntl
				state.GetRegistersRef()[4] = fd
				state.GetRegistersRef()[5] = cmd
				state.GetMemory().SetMemory(state.GetPC(), syscallInsn)
				step := state.GetStep()

				expected := testutil.CreateExpectedState(state)
				expected.Step += 1
				expected.PC = state.GetCpu().NextPC
				expected.NextPC = state.GetCpu().NextPC + 4
				if cmd == 3 {
					switch fd {
					case exec.FdStdin, exec.FdPreimageRead, exec.FdHintRead:
						expected.Registers[2] = 0
						expected.Registers[7] = 0
					case exec.FdStdout, exec.FdStderr, exec.FdPreimageWrite, exec.FdHintWrite:
						expected.Registers[2] = 1
						expected.Registers[7] = 0
					default:
						expected.Registers[2] = 0xFF_FF_FF_FF
						expected.Registers[7] = exec.MipsEBADF
					}
				} else {
					expected.Registers[2] = 0xFF_FF_FF_FF
					expected.Registers[7] = exec.MipsEINVAL
				}

				stepWitness, err := goVm.Step(true)
				require.NoError(t, err)
				require.False(t, stepWitness.HasPreimage())

				expected.Validate(t, state)

				evm := testutil.NewMIPSEVM(v.Contracts)
				evmPost := evm.Step(t, stepWitness, step, v.StateHashFn)
				goPost, _ := goVm.GetState().EncodeWitness()
				require.Equal(t, hexutil.Bytes(goPost).String(), hexutil.Bytes(evmPost).String(),
					"mipsevm produced different state than EVM")
			})
		}
	})
}

func FuzzStateHintRead(f *testing.F) {
	versions := GetMipsVersionTestCases(f)
	f.Fuzz(func(t *testing.T, addr uint32, count uint32, seed int64) {
		for _, v := range versions {
			t.Run(v.Name, func(t *testing.T) {
				preimageData := []byte("hello world")
				preimageKey := preimage.Keccak256Key(crypto.Keccak256Hash(preimageData)).PreimageKey()
				oracle := testutil.StaticOracle(t, preimageData) // only used for hinting

				goVm := v.VMFactory(oracle, os.Stdout, os.Stderr, testutil.CreateLogger(),
					testutil.WithRandomization(seed), testutil.WithPreimageKey(preimageKey))
				state := goVm.GetState()
				state.GetRegistersRef()[2] = exec.SysRead
				state.GetRegistersRef()[4] = exec.FdHintRead
				state.GetRegistersRef()[5] = addr
				state.GetRegistersRef()[6] = count
				state.GetMemory().SetMemory(state.GetPC(), syscallInsn)
				step := state.GetStep()

				expected := testutil.CreateExpectedState(state)
				expected.Step += 1
				expected.PC = state.GetCpu().NextPC
				expected.NextPC = state.GetCpu().NextPC + 4
				expected.Registers[2] = count
				expected.Registers[7] = 0 // no error

				stepWitness, err := goVm.Step(true)
				require.NoError(t, err)
				require.False(t, stepWitness.HasPreimage())

				expected.Validate(t, state)

				evm := testutil.NewMIPSEVM(v.Contracts)
				evmPost := evm.Step(t, stepWitness, step, v.StateHashFn)
				goPost, _ := goVm.GetState().EncodeWitness()
				require.Equal(t, hexutil.Bytes(goPost).String(), hexutil.Bytes(evmPost).String(),
					"mipsevm produced different state than EVM")
			})
		}
	})
}

func FuzzStatePreimageRead(f *testing.F) {
	versions := GetMipsVersionTestCases(f)
	f.Fuzz(func(t *testing.T, addr uint32, count uint32, preimageOffset uint32, seed int64) {
		for _, v := range versions {
			t.Run(v.Name, func(t *testing.T) {
				preimageValue := []byte("hello world")
				if preimageOffset >= uint32(len(preimageValue)) {
					t.SkipNow()
				}
				preimageKey := preimage.Keccak256Key(crypto.Keccak256Hash(preimageValue)).PreimageKey()
				oracle := testutil.StaticOracle(t, preimageValue)

				goVm := v.VMFactory(oracle, os.Stdout, os.Stderr, testutil.CreateLogger(),
					testutil.WithRandomization(seed), testutil.WithPreimageKey(preimageKey), testutil.WithPreimageOffset(preimageOffset))
				state := goVm.GetState()
				state.GetRegistersRef()[2] = exec.SysRead
				state.GetRegistersRef()[4] = exec.FdPreimageRead
				state.GetRegistersRef()[5] = addr
				state.GetRegistersRef()[6] = count
				state.GetMemory().SetMemory(state.GetPC(), syscallInsn)
				step := state.GetStep()

				alignment := addr & 3
				writeLen := 4 - alignment
				if count < writeLen {
					writeLen = count
				}
				// Cap write length to remaining bytes of the preimage
				preimageDataLen := uint32(len(preimageValue) + 8) // Data len includes a length prefix
				if preimageOffset+writeLen > preimageDataLen {
					writeLen = preimageDataLen - preimageOffset
				}

				expected := testutil.CreateExpectedState(state)
				expected.Step += 1
				expected.PC = state.GetCpu().NextPC
				expected.NextPC = state.GetCpu().NextPC + 4
				expected.Registers[2] = writeLen
				expected.Registers[7] = 0 // no error
				expected.PreimageOffset += writeLen

				stepWitness, err := goVm.Step(true)
				require.NoError(t, err)
				require.True(t, stepWitness.HasPreimage())

				// TODO(cp-983) - Do stricter validation of expected memory
				expected.Validate(t, state, testutil.SkipMemoryValidation)
				if writeLen == 0 {
					// Note: We are not asserting a memory root change when writeLen > 0 because we may not necessarily
					// modify memory - it's possible we just write the leading zero bytes of the length prefix
					require.Equal(t, expected.MemoryRoot, common.Hash(state.GetMemory().MerkleRoot()))
				}

				evm := testutil.NewMIPSEVM(v.Contracts)
				evmPost := evm.Step(t, stepWitness, step, v.StateHashFn)
				goPost, _ := goVm.GetState().EncodeWitness()
				require.Equal(t, hexutil.Bytes(goPost).String(), hexutil.Bytes(evmPost).String(),
					"mipsevm produced different state than EVM")
			})
		}
	})
}

func FuzzStateHintWrite(f *testing.F) {
	versions := GetMipsVersionTestCases(f)
	f.Fuzz(func(t *testing.T, addr uint32, count uint32, randSeed int64) {
		for _, v := range versions {
			t.Run(v.Name, func(t *testing.T) {
				preimageData := []byte("hello world")
				preimageKey := preimage.Keccak256Key(crypto.Keccak256Hash(preimageData)).PreimageKey()
				// TODO(cp-983) - use testutil.HintTrackingOracle, validate expected hints
				oracle := testutil.StaticOracle(t, preimageData) // only used for hinting

				goVm := v.VMFactory(oracle, os.Stdout, os.Stderr, testutil.CreateLogger(),
					testutil.WithRandomization(randSeed), testutil.WithPreimageKey(preimageKey))
				state := goVm.GetState()
				state.GetRegistersRef()[2] = exec.SysWrite
				state.GetRegistersRef()[4] = exec.FdHintWrite
				state.GetRegistersRef()[5] = addr
				state.GetRegistersRef()[6] = count
				step := state.GetStep()

				// Set random data at the target memory range
				randBytes := testutil.RandomBytes(t, randSeed, count)
				err := state.GetMemory().SetMemoryRange(addr, bytes.NewReader(randBytes))
				require.NoError(t, err)
				// Set instruction
				state.GetMemory().SetMemory(state.GetPC(), syscallInsn)

				expected := testutil.CreateExpectedState(state)
				expected.Step += 1
				expected.PC = state.GetCpu().NextPC
				expected.NextPC = state.GetCpu().NextPC + 4
				expected.Registers[2] = count
				expected.Registers[7] = 0 // no error

				stepWitness, err := goVm.Step(true)
				require.NoError(t, err)
				require.False(t, stepWitness.HasPreimage())

				// TODO(cp-983) - validate expected hints
				expected.Validate(t, state, testutil.SkipHintValidation)

				evm := testutil.NewMIPSEVM(v.Contracts)
				evmPost := evm.Step(t, stepWitness, step, v.StateHashFn)
				goPost, _ := goVm.GetState().EncodeWitness()
				require.Equal(t, hexutil.Bytes(goPost).String(), hexutil.Bytes(evmPost).String(),
					"mipsevm produced different state than EVM")
			})
		}
	})
}

func FuzzStatePreimageWrite(f *testing.F) {
	versions := GetMipsVersionTestCases(f)
	f.Fuzz(func(t *testing.T, addr uint32, count uint32, seed int64) {
		for _, v := range versions {
			t.Run(v.Name, func(t *testing.T) {
				preimageData := []byte("hello world")
				preimageKey := preimage.Keccak256Key(crypto.Keccak256Hash(preimageData)).PreimageKey()
				oracle := testutil.StaticOracle(t, preimageData)

				goVm := v.VMFactory(oracle, os.Stdout, os.Stderr, testutil.CreateLogger(),
					testutil.WithRandomization(seed), testutil.WithPreimageKey(preimageKey), testutil.WithPreimageOffset(128))
				state := goVm.GetState()
				state.GetRegistersRef()[2] = exec.SysWrite
				state.GetRegistersRef()[4] = exec.FdPreimageWrite
				state.GetRegistersRef()[5] = addr
				state.GetRegistersRef()[6] = count
				state.GetMemory().SetMemory(state.GetPC(), syscallInsn)
				step := state.GetStep()

				sz := 4 - (addr & 0x3)
				if sz < count {
					count = sz
				}

				expected := testutil.CreateExpectedState(state)
				expected.Step += 1
				expected.PC = state.GetCpu().NextPC
				expected.NextPC = state.GetCpu().NextPC + 4
				expected.PreimageOffset = 0
				expected.Registers[2] = count
				expected.Registers[7] = 0 // No error

				stepWitness, err := goVm.Step(true)
				require.NoError(t, err)
				require.False(t, stepWitness.HasPreimage())

				// TODO(cp-983) - validate preimage key
				expected.Validate(t, state, testutil.SkipPreimageKeyValidation)

				evm := testutil.NewMIPSEVM(v.Contracts)
				evmPost := evm.Step(t, stepWitness, step, v.StateHashFn)
				goPost, _ := goVm.GetState().EncodeWitness()
				require.Equal(t, hexutil.Bytes(goPost).String(), hexutil.Bytes(evmPost).String(),
					"mipsevm produced different state than EVM")
			})
		}
	})
}
