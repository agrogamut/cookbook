package portal

import (
	"encoding/json"
	"strings"
	"time"
)

func (s *Server) guardianNameForAccess(raw json.RawMessage) (name string, legacy, valid bool) {
	if len(raw) == 0 {
		legacy = time.Now().Before(s.options.LegacyGuardianUntil)
		return "", legacy, legacy
	}
	if json.Unmarshal(raw, &name) != nil {
		return "", false, false
	}
	name = strings.Join(strings.Fields(name), " ")
	return name, false, validName(name)
}
