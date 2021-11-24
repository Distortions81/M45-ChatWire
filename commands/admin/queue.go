package admin

import (
	"github.com/Distortions81/M45-ChatWire/fact"
	"github.com/bwmarrin/discordgo"
)

// Restart saves and restarts the server
func Queue(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {

	if !fact.IsQueued() {
		fact.CMS(m.ChannelID, "Reload is now queued.")
		fact.SetQueued(true)
	}
}
