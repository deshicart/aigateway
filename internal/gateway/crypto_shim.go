package gateway

import (
	"github.com/openbridge/gateway/internal/crypto"
)

func decryptWith(master []byte, enc string) (string, error) {
	b, err := crypto.Decrypt(master, enc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
