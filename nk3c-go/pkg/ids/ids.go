// Package ids ID 规范：int64 Snowflake，JSON 输出字符串（对应原规范“18 位 Long→String”【S6】）
package ids

import (
	"fmt"
	"sync"
	"time"
)

type ID int64

func (i ID) Int64() int64      { return int64(i) }
func (i ID) String() string    { return fmt.Sprintf("%d", int64(i)) }
func (i ID) MarshalJSON() ([]byte, error) { return []byte(`"` + i.String() + `"`), nil }

// Snowflake 极简实现：41bit 毫秒 + 10bit 节点 + 12bit 序列（单节点足够演示/生产起步）
type Snowflake struct {
	mu        sync.Mutex
	node      int64
	epoch     int64 // 2026-01-01 UTC
	lastMs    int64
	sequence  int64
}

func NewSnowflake(node int64) *Snowflake {
	return &Snowflake{node: node & 0x3FF, epoch: 1767225600000}
}

func (s *Snowflake) Next() ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	if now < s.lastMs {
		now = s.lastMs
	}
	if now == s.lastMs {
		s.sequence = (s.sequence + 1) & 0xFFF
		if s.sequence == 0 {
			for now <= s.lastMs {
				time.Sleep(200 * time.Microsecond)
				now = time.Now().UnixMilli()
			}
		}
	} else {
		s.sequence = 0
	}
	s.lastMs = now
	v := ((now - s.epoch) << 22) | (s.node << 12) | s.sequence
	return ID(v)
}
