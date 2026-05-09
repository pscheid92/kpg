package kpg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func StatePath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "kpg", StateFileName), nil
}

func WriteLastTarget(lt LastTarget) error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lt, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func ReadLastTarget() (LastTarget, error) {
	path, err := StatePath()
	if err != nil {
		return LastTarget{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return LastTarget{}, err
	}
	var lt LastTarget
	if err := json.Unmarshal(data, &lt); err != nil {
		return LastTarget{}, err
	}
	if lt.Namespace == "" || lt.Cluster == "" {
		return LastTarget{}, fmt.Errorf("invalid state in %s", path)
	}
	return lt, nil
}
