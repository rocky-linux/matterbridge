package bmattermost

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/42wim/matterbridge/bridge/config"
	"github.com/42wim/matterbridge/bridge/helper"
	"github.com/42wim/matterbridge/matterclient"
	"github.com/42wim/matterbridge/matterhook"
	matterclient6 "github.com/matterbridge/matterclient"
	"github.com/mattermost/mattermost-server/v5/model"
	model6 "github.com/mattermost/mattermost-server/v6/model"
)

func (b *Bmattermost) doConnectWebhookBind() error {
	switch {
	case b.GetString("WebhookURL") != "":
		b.Log.Info("Connecting using webhookurl (sending) and webhookbindaddress (receiving)")
		b.mh = matterhook.New(b.GetString("WebhookURL"),
			matterhook.Config{
				InsecureSkipVerify: b.GetBool("SkipTLSVerify"),
				BindAddress:        b.GetString("WebhookBindAddress"),
			})
	case b.GetString("Token") != "":
		b.Log.Info("Connecting using token (sending)")
		b.Log.Infof("Using mattermost v6 methods: %t", b.v6)

		if b.v6 {
			err := b.apiLogin6()
			if err != nil {
				return err
			}
		} else {
			err := b.apiLogin()
			if err != nil {
				return err
			}
		}
	case b.GetString("Login") != "":
		b.Log.Info("Connecting using login/password (sending)")
		b.Log.Infof("Using mattermost v6 methods: %t", b.v6)

		if b.v6 {
			err := b.apiLogin6()
			if err != nil {
				return err
			}
		} else {
			err := b.apiLogin()
			if err != nil {
				return err
			}
		}
	default:
		b.Log.Info("Connecting using webhookbindaddress (receiving)")
		b.mh = matterhook.New(b.GetString("WebhookURL"),
			matterhook.Config{
				InsecureSkipVerify: b.GetBool("SkipTLSVerify"),
				BindAddress:        b.GetString("WebhookBindAddress"),
			})
	}
	return nil
}

func (b *Bmattermost) doConnectWebhookURL() error {
	b.Log.Info("Connecting using webhookurl (sending)")
	b.mh = matterhook.New(b.GetString("WebhookURL"),
		matterhook.Config{
			InsecureSkipVerify: b.GetBool("SkipTLSVerify"),
			DisableServer:      true,
		})
	if b.GetString("Token") != "" {
		b.Log.Info("Connecting using token (receiving)")
		b.Log.Infof("Using mattermost v6 methods: %t", b.v6)

		if b.v6 {
			err := b.apiLogin6()
			if err != nil {
				return err
			}
		} else {
			err := b.apiLogin()
			if err != nil {
				return err
			}
		}
	} else if b.GetString("Login") != "" {
		b.Log.Info("Connecting using login/password (receiving)")
		b.Log.Infof("Using mattermost v6 methods: %t", b.v6)

		if b.v6 {
			err := b.apiLogin6()
			if err != nil {
				return err
			}
		} else {
			err := b.apiLogin()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Bmattermost) apiLogin() error {
	password := b.GetString("Password")
	if b.GetString("Token") != "" {
		password = "token=" + b.GetString("Token")
	}

	b.mc = matterclient.New(b.GetString("Login"), password, b.GetString("Team"), b.GetString("Server"))
	if b.GetBool("debug") {
		b.mc.SetLogLevel("debug")
	}
	b.mc.SkipTLSVerify = b.GetBool("SkipTLSVerify")
	b.mc.SkipVersionCheck = b.GetBool("SkipVersionCheck")
	b.mc.NoTLS = b.GetBool("NoTLS")
	b.Log.Infof("Connecting %s (team: %s) on %s", b.GetString("Login"), b.GetString("Team"), b.GetString("Server"))
	err := b.mc.Login()
	if err != nil {
		return err
	}
	b.Log.Info("Connection succeeded")
	b.TeamID = b.mc.GetTeamId()
	go b.mc.WsReceiver()
	go b.mc.StatusLoop()
	return nil
}

// nolint:wrapcheck
func (b *Bmattermost) apiLogin6() error {
	password := b.GetString("Password")
	if b.GetString("Token") != "" {
		password = "token=" + b.GetString("Token")
	}

	b.mc6 = matterclient6.New(b.GetString("Login"), password, b.GetString("Team"), b.GetString("Server"), "")
	if b.GetBool("debug") {
		b.mc6.SetLogLevel("debug")
	}
	b.mc6.SkipTLSVerify = b.GetBool("SkipTLSVerify")
	b.mc6.SkipVersionCheck = b.GetBool("SkipVersionCheck")
	b.mc6.NoTLS = b.GetBool("NoTLS")
	b.Log.Infof("Connecting %s (team: %s) on %s", b.GetString("Login"), b.GetString("Team"), b.GetString("Server"))

	if err := b.mc6.Login(); err != nil {
		return err
	}

	b.Log.Info("Connection succeeded")
	b.TeamID = b.mc6.GetTeamID()
	return nil
}

// replaceAction replace the message with the correct action (/me) code
func (b *Bmattermost) replaceAction(text string) (string, bool) {
	if strings.HasPrefix(text, "*") && strings.HasSuffix(text, "*") {
		return strings.Replace(text, "*", "", -1), true
	}
	return text, false
}

func (b *Bmattermost) cacheAvatar(msg *config.Message) (string, error) {
	fi := msg.Extra["file"][0].(config.FileInfo)
	/* if we have a sha we have successfully uploaded the file to the media server,
	so we can now cache the sha */
	if fi.SHA != "" {
		b.Log.Debugf("Added %s to %s in avatarMap", fi.SHA, msg.UserID)
		b.avatarMap[msg.UserID] = fi.SHA
	}
	return "", nil
}

// sendWebhook uses the configured WebhookURL to send the message
func (b *Bmattermost) sendWebhook(msg config.Message) (string, error) {
	// skip events
	if msg.Event != "" {
		return "", nil
	}

	if b.GetBool("PrefixMessagesWithNick") {
		msg.Text = msg.Username + msg.Text
	}
	if msg.Extra != nil {
		// this sends a message only if we received a config.EVENT_FILE_FAILURE_SIZE
		for _, rmsg := range helper.HandleExtra(&msg, b.General) {
			rmsg := rmsg // scopelint
			iconURL := config.GetIconURL(&rmsg, b.GetString("iconurl"))
			matterMessage := matterhook.OMessage{
				IconURL:  iconURL,
				Channel:  rmsg.Channel,
				UserName: rmsg.Username,
				Text:     rmsg.Text,
				Props:    make(map[string]interface{}),
			}
			matterMessage.Props["matterbridge_"+b.uuid] = true
			if err := b.mh.Send(matterMessage); err != nil {
				b.Log.Errorf("sendWebhook failed: %s ", err)
			}
		}

		// webhook doesn't support file uploads, so we add the url manually
		if len(msg.Extra["file"]) > 0 {
			for _, f := range msg.Extra["file"] {
				fi := f.(config.FileInfo)
				if fi.URL != "" {
					msg.Text += fi.URL
				}
			}
		}
	}

	iconURL := config.GetIconURL(&msg, b.GetString("iconurl"))
	matterMessage := matterhook.OMessage{
		IconURL:  iconURL,
		Channel:  msg.Channel,
		UserName: msg.Username,
		Text:     msg.Text,
		Props:    make(map[string]interface{}),
	}
	if msg.Avatar != "" {
		matterMessage.IconURL = msg.Avatar
	}
	matterMessage.Props["matterbridge_"+b.uuid] = true
	err := b.mh.Send(matterMessage)
	if err != nil {
		b.Log.Info(err)
		return "", err
	}
	return "", nil
}

// skipMessages returns true if this message should not be handled
func (b *Bmattermost) skipMessage(message *matterclient.Message) bool {
	// Handle join/leave
	if message.Type == "system_join_leave" ||
		message.Type == "system_join_channel" ||
		message.Type == "system_leave_channel" {
		if b.GetBool("nosendjoinpart") {
			return true
		}
		b.Log.Debugf("Sending JOIN_LEAVE event from %s to gateway", b.Account)
		b.Remote <- config.Message{
			Username: "system",
			Text:     message.Text,
			Channel:  message.Channel,
			Account:  b.Account,
			Event:    config.EventJoinLeave,
		}
		return true
	}

	// Handle edited messages
	if (message.Raw.Event == model.WEBSOCKET_EVENT_POST_EDITED) && b.GetBool("EditDisable") {
		return true
	}

	// Ignore non-post messages
	if message.Post == nil {
		b.Log.Debugf("ignoring nil message.Post: %#v", message)
		return true
	}

	// Ignore messages sent from matterbridge
	if message.Post.Props != nil {
		if _, ok := message.Post.Props["matterbridge_"+b.uuid].(bool); ok {
			b.Log.Debugf("sent by matterbridge, ignoring")
			return true
		}
	}

	// Ignore messages sent from a user logged in as the bot
	if b.mc.User.Username == message.Username {
		return true
	}

	// if the message has reactions don't repost it (for now, until we can correlate reaction with message)
	if message.Post.HasReactions {
		return true
	}

	// ignore messages from other teams than ours
	if message.Raw.Data["team_id"].(string) != b.TeamID {
		return true
	}

	// only handle posted, edited or deleted events
	if !(message.Raw.Event == "posted" || message.Raw.Event == model.WEBSOCKET_EVENT_POST_EDITED ||
		message.Raw.Event == model.WEBSOCKET_EVENT_POST_DELETED) {
		return true
	}
	return false
}

// skipMessages returns true if this message should not be handled
// nolint:gocyclo,cyclop
func (b *Bmattermost) skipMessage6(message *matterclient6.Message) bool {
	// Handle join/leave
	if message.Type == "system_join_leave" ||
		message.Type == "system_join_channel" ||
		message.Type == "system_leave_channel" {
		if b.GetBool("nosendjoinpart") {
			return true
		}
		b.Log.Debugf("Sending JOIN_LEAVE event from %s to gateway", b.Account)
		b.Remote <- config.Message{
			Username: "system",
			Text:     message.Text,
			Channel:  message.Channel,
			Account:  b.Account,
			Event:    config.EventJoinLeave,
		}
		return true
	}

	// Handle edited messages
	if (message.Raw.EventType() == model6.WebsocketEventPostEdited) && b.GetBool("EditDisable") {
		return true
	}

	// Ignore non-post messages
	if message.Post == nil {
		b.Log.Debugf("ignoring nil message.Post: %#v", message)
		return true
	}

	// Ignore messages sent from matterbridge
	if message.Post.Props != nil {
		if _, ok := message.Post.Props["matterbridge_"+b.uuid].(bool); ok {
			b.Log.Debugf("sent by matterbridge, ignoring")
			return true
		}
	}

	// Ignore messages sent from a user logged in as the bot
	if b.mc6.User.Username == message.Username {
		return true
	}

	// if the message has reactions don't repost it (for now, until we can correlate reaction with message)
	if message.Post.HasReactions {
		return true
	}

	// ignore messages from other teams than ours
	if message.Raw.GetData()["team_id"].(string) != b.TeamID {
		return true
	}

	// only handle posted, edited or deleted events
	if !(message.Raw.EventType() == "posted" || message.Raw.EventType() == model6.WebsocketEventPostEdited ||
		message.Raw.EventType() == model6.WebsocketEventPostDeleted) {
		return true
	}
	return false
}

// Extracts the old and new topics out of a a system "topic_change" notification
func (b *Bmattermost) extractTopic(topic string) (topicold string, topicnew string) {
	//[0018] DEBUG irc:          [Send:bridge/irc/irc.go:141] => Receiving config.Message{Text:"neil updated the channel header from: bananananananaana to: bananana2", Channel:"##potato", Username:"<neil> ", UserID:"x1wkd3eeepgafb536qg3bemncw", Avatar:"", Account:"mattermost.potato", Event:"topic_change", Protocol:"mattermost", Gateway:"gateway1", ParentID:"", Timestamp:time.Date(2021, time.November, 18, 14, 47, 15, 943182536, time.Local), ID:"", Extra:map[string][]interface {}{}}

	var pattern = regexp.MustCompile(`^(?P<username>[\w\s]+)\s?updated the channel header from:\s?(?P<topicold>.*)\sto:\s?(?P<topicnew>.*)$`)
	match := pattern.FindStringSubmatch(topic)

	// Map capture groups into a map like result["username"], result["topicold"]
	result := make(map[string]string)
	for i, name := range pattern.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}

	topicold, topicnew = strings.TrimSpace(result["topicold"]), strings.TrimSpace(result["topicnew"])
	b.Log.Debugf("extracted old topic '%s' and new topic '%s'", topicold, topicnew)
	return
}

func (b *Bmattermost) getVersion() string {
	proto := "https"

	if b.GetBool("notls") {
		proto = "http"
	}

	resp, err := http.Get(proto + "://" + b.GetString("server"))
	if err != nil {
		b.Log.Error("failed getting version")
		return ""
	}

	defer resp.Body.Close()

	return resp.Header.Get("X-Version-Id")
}

func (b *Bmattermost) isWebhookClient() bool {
	if b.GetString("WebhookURL") == "" && b.GetString("WebhookBindAddress") == "" {
		return false
	}
	return true
}

func (b *Bmattermost) getChannelID(channel string) (string, error) {
	var id string
	if !b.isWebhookClient() {
		if b.mc6 != nil {
			id = b.mc6.GetChannelID(channel, b.TeamID)
		} else {
			id = b.mc.GetChannelId(channel, b.TeamID)
		}
		if id == "" {
			return "", fmt.Errorf("could not find channel ID for channel %s", channel)
		}
	}
	return id, nil
}

func (b *Bmattermost) changeChannelHeader(message config.Message) (bool, error) {
	// not sure why this is here, assuming best practice from Bmattermost.JoinChannel()
	if b.Account == mattermostPlugin {
		return false, fmt.Errorf("ignoring this condition")
	}

	id, err := b.getChannelID(message.Channel)
	if err != nil {
		return false, err
	}

	var header = strings.TrimRight(message.Text, " ")

	if b.mc6 != nil {
		b.mc6.UpdateChannelHeader(id, header)
	} else {
		b.mc.UpdateChannelHeader(id, header)
	}
	return true, nil
}
