package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// As in Kivrix, the encryption key is local to this OS account. It protects
// configuration copied without the key, not access by another process running
// as the same user. Keep secrets.key with the database when making backups.
type encryptedSettings struct {
	Version   int    `json:"version"`
	Encrypted []byte `json:"encrypted"`
}

var settingsAAD = []byte("panreel-search-settings-v2")

func (s *Store) settingsCipher(create bool) (cipher.AEAD, error) {
	path := filepath.Join(s.Dir, "secrets.key")
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) && create {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		var f *os.File
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			_, err = f.Write(key)
			if err == nil {
				err = f.Sync()
			}
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		} else if os.IsExist(err) {
			key, err = os.ReadFile(path)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read local configuration key: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("invalid local configuration key; restore secrets.key from your backup")
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *Store) sealSettings(data []byte, create bool) ([]byte, error) {
	aead, err := s.settingsCipher(create)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := aead.Seal(nonce, nonce, data, settingsAAD)
	return json.Marshal(encryptedSettings{Version: 2, Encrypted: sealed})
}

func (s *Store) openSettings(data []byte) ([]byte, bool, error) {
	var envelope encryptedSettings
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, false, err
	}
	if envelope.Version == 0 && envelope.Encrypted == nil {
		return data, true, nil
	}
	if envelope.Version != 2 {
		return nil, false, errors.New("unsupported configuration version")
	}
	aead, err := s.settingsCipher(false)
	if err != nil {
		return nil, false, err
	}
	if len(envelope.Encrypted) < aead.NonceSize()+aead.Overhead() {
		return nil, false, errors.New("incomplete encrypted configuration")
	}
	plain, err := aead.Open(nil, envelope.Encrypted[:aead.NonceSize()], envelope.Encrypted[aead.NonceSize():], settingsAAD)
	if err != nil {
		return nil, false, errors.New("configuration could not be decrypted; restore the matching secrets.key and database")
	}
	return plain, false, nil
}
