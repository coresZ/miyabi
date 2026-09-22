package api

import (
	"crypto/sha256"
	"crypto/subtle"

	"github.com/ppxb/miyabi/internal/domain"
)

var ErrAccessPassword = domain.E(domain.KindUnauthorized, "访问密码错误", nil)

// AccessGateService checks the optional Web entry password, as in jm-boom.
// It does not authenticate subsequent API requests or create server sessions.
type AccessGateService struct {
	enabled      bool
	passwordHash [sha256.Size]byte
}

func NewAccessGateService(password string) *AccessGateService {
	return &AccessGateService{
		enabled:      password != "",
		passwordHash: sha256.Sum256([]byte(password)),
	}
}

func (gate *AccessGateService) Enabled() bool {
	return gate.enabled
}

func (gate *AccessGateService) Verify(password string) error {
	if !gate.enabled {
		return nil
	}
	actual := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(gate.passwordHash[:], actual[:]) != 1 {
		return ErrAccessPassword
	}
	return nil
}
