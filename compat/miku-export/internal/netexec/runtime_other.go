//go:build !linux

package netexec

func Open(instanceID string, mutating bool) (*Engine, error) {
	if err := ValidateID(instanceID); err != nil {
		return nil, err
	}
	return nil, ErrUnsupported
}
