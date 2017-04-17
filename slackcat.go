package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/bluele/slack"
)

//SlackCat client
type SlackCat struct {
	api         *slack.Slack
	opts        *slack.ChatPostMessageOpt
	queue       *StreamQ
	shutdown    chan os.Signal
	channelID   string
	channelName string
}

func newSlackCat(token, channelName string) *SlackCat {
	sc := &SlackCat{
		api:         slack.New(token),
		opts:        &slack.ChatPostMessageOpt{AsUser: true},
		queue:       newStreamQ(),
		shutdown:    make(chan os.Signal, 1),
		channelName: channelName,
	}

	res, err := sc.api.AuthTest()
	failOnError(err, "Slack API Error", true)
	output(fmt.Sprintf("connected to %s as %s", res.Team, res.User))
	sc.channelID = sc.lookupSlackID()

	signal.Notify(sc.shutdown, os.Interrupt)
	return sc
}

//Lookup Slack id for channel, group, or im by name
func (sc *SlackCat) lookupSlackID() string {
	api := sc.api
	if channel, err := api.FindChannelByName(sc.channelName); err == nil {
		return channel.Id
	}
	if group, err := api.FindGroupByName(sc.channelName); err == nil {
		return group.Id
	}
	if im, err := api.FindImByName(sc.channelName); err == nil {
		return im.Id
	}
	exitErr(fmt.Errorf("No such channel, group, or im"))
	return ""
}

func (sc *SlackCat) trap() {
	sigcount := 0
	for sig := range sc.shutdown {
		if sigcount > 0 {
			exitErr(fmt.Errorf("aborted"))
		}
		output(fmt.Sprintf("got signal: %s", sig.String()))
		output("press ctrl+c again to exit immediately")
		sigcount++
		go sc.exit()
	}
}

func (sc *SlackCat) exit() {
	for {
		if sc.queue.IsEmpty() {
			os.Exit(0)
		} else {
			output("flushing remaining messages to Slack...")
			time.Sleep(3 * time.Second)
		}
	}
}

func (sc *SlackCat) addToStreamQ(lines chan string) {
	for line := range lines {
		sc.queue.Add(line)
	}
	sc.exit()
}

//TODO: handle messages with length exceeding maximum for Slack chat
func (sc *SlackCat) processStreamQ() {
	if !(sc.queue.IsEmpty()) {
		msglines := sc.queue.Flush()
		if noop {
			output(fmt.Sprintf("skipped posting of %s message lines to %s", strconv.Itoa(len(msglines)), sc.channelName))
		} else {
			sc.postMsg(msglines)
		}
		sc.queue.Ack()
	}
	time.Sleep(3 * time.Second)
	sc.processStreamQ()
}

func (sc *SlackCat) postMsg(msglines []string) {
	msg := strings.Join(msglines, "\n")
	msg = strings.Replace(msg, "&", "%26amp%3B", -1)
	msg = strings.Replace(msg, "<", "%26lt%3B", -1)
	msg = strings.Replace(msg, ">", "%26gt%3B", -1)

	err := sc.api.ChatPostMessage(sc.channelID, msg, sc.opts)
	failOnError(err, "", true)
	count := strconv.Itoa(len(msglines))
	output(fmt.Sprintf("posted %s message lines to %s", count, sc.channelName))
}

func (sc *SlackCat) postFile(filePath, fileName, fileType, fileComment string) {
	//default to timestamp for filename
	if fileName == "" {
		fileName = strconv.FormatInt(time.Now().Unix(), 10)
	}

	if noop {
		output(fmt.Sprintf("skipping upload of file %s to %s", fileName, sc.channelName))
		return
	}

	start := time.Now()
	err := sc.api.FilesUpload(&slack.FilesUploadOpt{
		Filepath:       filePath,
		Filename:       fileName,
		Filetype:       fileType,
		Title:          fileName,
		InitialComment: fileComment,
		Channels:       []string{sc.channelID},
	})
	failOnError(err, "error uploading file to Slack", true)
	duration := strconv.FormatFloat(time.Since(start).Seconds(), 'f', 3, 64)
	output(fmt.Sprintf("file %s uploaded to %s (%ss)", fileName, sc.channelName, duration))
	os.Exit(0)
}
