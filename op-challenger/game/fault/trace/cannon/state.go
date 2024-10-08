package cannon

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/ethereum-optimism/optimism/cannon/mipsevm/singlethreaded"
	"github.com/ethereum-optimism/optimism/op-service/ioutil"
)

func parseState(path string) (*singlethreaded.State, error) {
	file, err := ioutil.OpenDecompressed(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open state file (%v): %w", path, err)
	}
	return parseStateFromReader(file)
}

func parseStateFromReader(in io.ReadCloser) (*singlethreaded.State, error) {
	defer in.Close()
	var state singlethreaded.State
	if err := json.NewDecoder(in).Decode(&state); err != nil {
		return nil, fmt.Errorf("invalid mipsevm state: %w", err)
	}
	return &state, nil
}
