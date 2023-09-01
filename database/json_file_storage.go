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

var _ RelayerDatabase = &JSONFileStorage{}

type chainState map[string]string

// JSONFileStorage implements RelayerDatabase
type JSONFileStorage struct {
	// the directory where the json files are stored
	dir string

	// Each network has its own mutex
	// The chainIDs used to index the JSONFileStorage are created at initialization
	// and are not modified afterwards, so we don't need to lock the map itself.
	mutexes map[ids.ID]*sync.RWMutex
	logger  logging.Logger
}

// NewJSONFileStorage creates a new JSONFileStorage instance
func NewJSONFileStorage(logger logging.Logger, dir string, networks []ids.ID) (*JSONFileStorage, error) {
	storage := &JSONFileStorage{
		dir:     filepath.Clean(dir),
		mutexes: make(map[ids.ID]*sync.RWMutex),
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
		storage.logger.Error("failed to create directory",
			zap.String("dir", dir),
			zap.Error(err))
		return nil, err
	}

	return storage, nil
}

// Get the latest chain state from the json database, and retrieve the value from the key
func (s *JSONFileStorage) Get(chainID ids.ID, key []byte) ([]byte, error) {
	mutex, ok := s.mutexes[chainID]
	if !ok {
		return nil, errors.New("network does not exist")
	}

	mutex.RLock()
	defer mutex.RUnlock()
	currentState := make(chainState)
	fileExists, err := s.read(chainID, &currentState)
	if err != nil {
		s.logger.Error(
			"failed to read file",
			zap.String("chainID", chainID.String()),
			zap.Error(err),
		)
		return nil, err
	}
	if !fileExists {
		return nil, errors.Errorf("file does not exist. chainID: %s", chainID)
	}

	if val, ok := currentState[string(key)]; ok {
		return []byte(val), nil
	}

	return nil, errors.Errorf("key does not exist. chainID: %s key: %s", chainID, key)
}

// Put the value into the json database. Read the current chain state and overwrite the key, if it exists
func (s *JSONFileStorage) Put(chainID ids.ID, key []byte, value []byte) error {
	mutex, ok := s.mutexes[chainID]
	if !ok {
		return errors.Errorf("network does not exist. chainID: %s", chainID.String())
	}

	mutex.Lock()
	defer mutex.Unlock()

	currentState := make(chainState)
	_, err := s.read(chainID, &currentState)
	if err != nil {
		s.logger.Error(
			"failed to read file",
			zap.String("chainID", chainID.String()),
			zap.Error(err),
		)
		return err
	}

	currentState[string(key)] = string(value)

	return s.write(chainID, currentState)
}

// Write the value to the file. The caller is responsible for ensuring proper synchronization
func (s *JSONFileStorage) write(network ids.ID, v interface{}) error {
	fnlPath := filepath.Join(s.dir, network.String()+".json")
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
// Returns a bool indicating whether the file exists, and an error.
// If an error is returned, the bool should be ignored.
// The caller is responsible for ensuring proper synchronization
func (s *JSONFileStorage) read(network ids.ID, v interface{}) (bool, error) {
	path := filepath.Join(s.dir, network.String()+".json")

	// If the file does not exist, return false, but do not return an error as this
	// is an expected case
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		s.logger.Debug(
			"file does not exist",
			zap.String("path", path),
			zap.Error(err),
		)
		return false, nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return false, errors.Wrap(err, "failed to read file")
	}

	// unmarshal data
	if err = json.Unmarshal(b, &v); err != nil {
		return false, errors.Wrap(err, "failed to unmarshal json file")
	}

	// Return true to indicate that the file exists and we read from it successfully
	return true, nil
}
