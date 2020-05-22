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

	w, err := bot.Start(os.Getenv("BOT_TOKEN"), &Commands{}, func(ctx *bot.Context) error {
		ctx.HasPrefix = bot.NewPrefix("!")
		return nil
	})
	if err != nil {
		log.Fatalln(err)
	}
	w()
}

type Commands struct {
	Ctx *bot.Context

	subscriptionMu sync.Mutex
	subscriptions  map[discord.Snowflake]*channelSubscription
}

type channelSubscription struct {
	dura time.Duration
	last time.Time
}

type Filename string

func (c *Commands) Upload(m *gateway.MessageCreateEvent, fn Filename) error {
	// Sanitize filepath.
	name := filepath.Base(filepath.Clean(string(fn)))

	return c.uploadTo(m.ChannelID, name)
}

func (c *Commands) Random(m *gateway.MessageCreateEvent) error {
	return c.randomTo(m.ChannelID)
}

func (c *Commands) Subscribe(m *gateway.MessageCreateEvent, dura Duration) error {
	s, err := c.Ctx.SendText(m.ChannelID, "Test.")
	if err != nil {
		// This would probably not send, but whatever.
		return errors.Wrap(err, "Failed to send a message")
	}

	c.subscriptionMu.Lock()
	c.subscriptions[m.ChannelID] = &channelSubscription{
		dura: time.Duration(dura),
		last: time.Now(),
	}
	c.subscriptionMu.Unlock()

	c.Ctx.EditText(m.ChannelID, s.ID, "Subscribed.")
	return nil
}

func (c *Commands) Unsubscribe(m *gateway.MessageCreateEvent) error {
	c.subscriptionMu.Lock()
	defer c.subscriptionMu.Unlock()

	if _, ok := c.subscriptions[m.ChannelID]; !ok {
		return errors.New("You're not subscribed.")
	}
	delete(c.subscriptions, m.ChannelID)
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
	for range time.Tick(5 * time.Second) {
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