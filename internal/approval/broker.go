package approval

import (
	"strings"
	"time"
)

func (b *Broker) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

func (b *Broker) hostID() string {
	if strings.TrimSpace(b.HostID) != "" {
		return strings.TrimSpace(b.HostID)
	}
	return strings.TrimSpace(b.Config.HostID)
}
