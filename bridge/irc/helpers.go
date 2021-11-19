package birc

import "strings"

func (b *Birc) cleanTopic(topic string) string {
	return strings.ReplaceAll(topic, "\n", "")
}
