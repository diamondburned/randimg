package main

import (
	"flag"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/diamondburned/arikawa/api"
	"github.com/diamondburned/arikawa/bot"
	"github.com/diamondburned/arikawa/bot/extras/arguments"
	"github.com/diamondburned/arikawa/discord"
	"github.com/diamondburned/arikawa/gateway"
	"github.com/pkg/errors"
)

// MinDuration is the fastest delay before the next image.
const MinDuration = time.Minute

var cwd = "./"

func main() {
	flag.StringVar(&cwd, "d", cwd, "The directory to get files from.")
	flag.Parse()

	rand.Seed(time.Now().UnixNano())

	cmd := &Commands{}
	go cmd.startWorker()

	w, err := bot.Start(os.Getenv("BOT_TOKEN"), cmd, func(ctx *bot.Context) error {
		ctx.HasPrefix = bot.NewPrefix("!")
		return nil
	})
	if err != nil {
		log.Fatalln(err)
	}
	w()
}

type channelSubscription struct {
	dura time.Duration
	last time.Time
}

type Commands struct {
	Ctx *bot.Context

	subscriptionMu sync.Mutex
	subscriptions  map[discord.Snowflake]*channelSubscription
}

func NewCommands() *Commands {
	return &Commands{
		subscriptions: make(map[discord.Snowflake]*channelSubscription),
	}
}

func (c *Commands) Upload(m *gateway.MessageCreateEvent, fn Filename) error {
	return c.uploadTo(m.ChannelID, string(fn))
}

func (c *Commands) Random(m *gateway.MessageCreateEvent) error {
	return c.randomTo(m.ChannelID)
}

func (c *Commands) Subscribe(
	m *gateway.MessageCreateEvent, ch arguments.ChannelMention, dura Duration) (string, error) {

	chID := discord.Snowflake(ch)

	s, err := c.Ctx.SendText(chID, "Test.")
	if err != nil {
		// This would probably not send, but whatever.
		return "", errors.Wrap(err, "Failed to send a message")
	}

	c.Ctx.DeleteMessage(chID, s.ID)

	c.subscriptionMu.Lock()
	c.subscriptions[m.ChannelID] = &channelSubscription{
		dura: time.Duration(dura),
		last: time.Now(),
	}
	c.subscriptionMu.Unlock()

	return "Subscribed.", nil
}

func (c *Commands) Unsubscribe(m *gateway.MessageCreateEvent, ch arguments.ChannelMention) error {
	chID := discord.Snowflake(ch)

	c.subscriptionMu.Lock()
	defer c.subscriptionMu.Unlock()

	if _, ok := c.subscriptions[chID]; !ok {
		return errors.New("You're not subscribed.")
	}
	delete(c.subscriptions, chID)
	return nil
}

func (c *Commands) randomTo(chIDs ...discord.Snowflake) error {
	d, err := ioutil.ReadDir(cwd)
	if err != nil {
		return errors.Wrap(err, "Failed to read directory")
	}

	for _, chID := range chIDs {
		// Pick a random file.
		name := d[rand.Intn(len(d))].Name()

		if err := c.uploadTo(chID, name); err != nil && len(chIDs) == 1 {
			return err
		}
	}

	// Ignore errors if we have more than 1 channel given.
	return nil
}

func (c *Commands) uploadTo(chID discord.Snowflake, name string) error {
	f, err := os.Open(filepath.Join(cwd, name))
	if err != nil {
		return errors.New("Path not found.")
	}
	defer f.Close()

	_, err = c.Ctx.SendMessageComplex(chID, api.SendMessageData{
		Files: []api.SendMessageFile{{
			Name:   f.Name(),
			Reader: f,
		}},
	})
	return errors.Wrap(err, "Failed to send message")
}

func (c *Commands) startWorker() {
	for range time.Tick(15 * time.Second) {
		channelIDs := c.pollSubscribes()
		if len(channelIDs) > 0 {
			go c.randomTo(channelIDs...)
		}
	}
}

func (c *Commands) pollSubscribes() (sendTo []discord.Snowflake) {
	c.subscriptionMu.Lock()
	defer c.subscriptionMu.Unlock()

	now := time.Now()

	for chID, sub := range c.subscriptions {
		if sub.last.Add(sub.dura).After(now) {
			sendTo = append(sendTo, chID)
			sub.last = now
		}
	}

	return
}

type Filename string

func (f *Filename) Parse(str string) error {
	// Sanitize filepath.
	*f = Filename(filepath.Base(filepath.Clean(str)))
	return nil
}

type Duration time.Duration

func (dura *Duration) Parse(content string) error {
	d, err := time.ParseDuration(content)
	if err != nil {
		return err
	}
	if d < MinDuration {
		return errors.New("Duration is too short, pick something longer.")
	}
	*dura = Duration(d)
	return nil
}
