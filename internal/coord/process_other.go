//go:build !linux && !darwin

package coord

func processStart(int) (string, error) { return "", ErrUnsupported }
