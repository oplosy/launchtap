package social

import (
	"errors"
	"strings"
)

var (
	ErrInvalidUsername  = errors.New("invalid username")
	ErrUsernameReserved = errors.New("username reserved")
)

var reservedUsernames = map[string]struct{}{
	"admin": {}, "administrator": {}, "launchpad": {}, "support": {}, "moderator": {}, "mod": {},
	"official": {}, "team": {}, "staff": {}, "system": {}, "root": {}, "help": {}, "security": {},
	"robinhood": {}, "pons": {}, "null": {}, "undefined": {}, "anonymous": {}, "deleted": {},
	"operator": {},
}

// NormalizeUsername applies the spec section "Username rules": trim and lowercase; 3-20
// characters of [a-z0-9_]; no 0x prefix (it would imitate an address); and not reserved, where a
// name is reserved when, with underscores removed and trailing digits trimmed, it equals a
// reserved word. It returns ErrInvalidUsername or ErrUsernameReserved.
func NormalizeUsername(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if len(name) < 3 || len(name) > 20 || strings.HasPrefix(name, "0x") {
		return "", ErrInvalidUsername
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return "", ErrInvalidUsername
		}
	}
	key := strings.TrimRight(strings.ReplaceAll(name, "_", ""), "0123456789")
	if _, reserved := reservedUsernames[key]; reserved {
		return "", ErrUsernameReserved
	}
	return name, nil
}
