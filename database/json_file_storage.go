package database

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var _ RelayerDatabase = &JsonFileStorage{}

type chainState map[string]string

// JsonFileStorage implements RelayerDatabase
type JsonFileStorage struct {
	// the directory where the json files are stored
	dir string

	// one mutex per network
	// we predefined the network when creating the storage
	// also we don't allow adding new network after the storage is created
	// so we don't need to lock the map
	mutexes map[string]*sync.RWMutex
	logger  logging.Logger
}

// NewJSONFileStorage creates a new JsonFileStorage instance
// with predefined networks. e.g "ethereum", "avalanche"
func NewJSONFileStorage(logger logging.Logger, dir string, networks []string) (*JsonFileStorage, error) {
	storage := &JsonFileStorage{
		dir:     filepath.Clean(dir),
		mutexes: make(map[string]*sync.RWMutex),
		logger:  logger,
	}

	for _, network := range networks {
		storage.mutexes[network] = &sync.RWMutex{}
	}

	_, err := os.Stat(dir)
	if err == nil {
		// dir already exist
		// return the existing storage
		return storage, nil
	}

	// 0755: The owner can read, write, execute.
	// Everyone else can read and execute but not modify the file.
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		storage.logger.Error("failed to create dir",
			zap.Error(err))
		return nil, err
	}

	return storage, nil
}

// Get the latest chain state from the json database, and retrieve the value from the key
func (s *JsonFileStorage) Get(chainID ids.ID, key []byte) ([]byte, error) {
	var currentState chainState
	err := s.read(chainID.String(), &currentState)
	if err != nil {
		return nil, err
	}

	return []byte(currentState[string(key)]), nil
}

// Put the value into the json database. Read the current chain state and overwrite the key, if it exists
func (s *JsonFileStorage) Put(chainID ids.ID, key []byte, value []byte) error {
	currentState := make(chainState)
	var currentStateData []byte
	err := s.read(chainID.String(), &currentStateData)
	if err == nil {
		err = json.Unmarshal(currentStateData, &currentState)
		if err != nil {
			return err
		}
	}

	currentState[string(key)] = string(value)

	return s.write(chainID.String(), currentState)
}

// write value into the file
func (s *JsonFileStorage) write(network string, v interface{}) error {
	mutex, ok := s.mutexes[network]
	if !ok {
		return errors.New("network does not exist")
	}

	mutex.Lock()
	defer mutex.Unlock()

	fnlPath := filepath.Join(s.dir, network+".json")
	tmpPath := fnlPath + ".tmp"

	b, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return err
	}

	// write marshaled data to the temp file
	// if write failed, the original file is not affected
	// 0644 Only the owner can read and write.
	// Everyone else can only read. No one can execute the file.
	if err := os.WriteFile(tmpPath, b, 0644); err != nil {
		return errors.Wrap(err, "failed to write file")
	}

	// move final file into place
	if err := os.Rename(tmpPath, fnlPath); err != nil {
		return errors.Wrap(err, "failed to rename file")
	}

	return nil
}

// Read from disk and unmarshal into v
func (s *JsonFileStorage) read(network string, v interface{}) error {
	mutex, ok := s.mutexes[network]
	if !ok {
		return errors.New("network does not exist")
	}

	mutex.RLock()
	defer mutex.RUnlock()

	path := filepath.Join(s.dir, network+".json")
	_, err := os.Stat(path)
	if err != nil {
		return err
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrap(err, "failed to read file")
	}

	// unmarshal data
	if err = json.Unmarshal(b, &v); err != nil {
		return errors.Wrap(err, "failed to unmarshal json file")
	}

	return nil
}
