package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gen1us2k/log"
	"github.com/nlopes/slack"
)

type SlackService struct {
	BaseService
	logger   log.Logger
	sh       *SlackHistoryBot
	rtm      *slack.RTM
	search   *SearchService
	slackAPI *slack.Client
	me       string
}

func (ss *SlackService) Name() string {
	return "slack_worker"
}

func (ss *SlackService) Init(sh *SlackHistoryBot) error {
	ss.sh = sh
	ss.logger = log.NewLogger(ss.Name())
	api := slack.New(ss.sh.Config().SlackToken)
	ss.rtm = api.NewRTM()
	ss.search = ss.sh.SearchService()
	ss.slackAPI = api
	return nil
}

func (ss *SlackService) Run() error {
	go ss.rtm.ManageConnection()
	for {
		if ss.me == "" {
			me := ss.rtm.GetInfo()
			if me != nil {
				ss.me = me.User.ID
				ss.logger.Infof("I've found myself: %s", me.User.ID)
			}
		}
		select {
		case msg := <-ss.rtm.IncomingEvents:
			switch ev := msg.Data.(type) {
			case *slack.ConnectedEvent:
				ss.logger.Info("Bot connected!")
			case *slack.MessageEvent:
				if ss.isToMe(ev.Msg.Text) {
					ss.logger.Infof("Searching query %s for channel %s", ev.Text, ev.Channel)
					found := false
					res, err := ss.search.Search(ss.cleanMessage(ev.Text), ev.Channel)
					if err != nil {
						ss.logger.Error(err)
					}
					var results []string

					for _, item := range res.Hits {
						result := item.Fields
						if result["channel"] == ev.Channel {
							user := ss.rtm.GetInfo().GetUserByID(result["username"].(string))
							if user != nil {
								times := strings.Split(result["timestamp"].(string), ".")
								i, err := strconv.ParseInt(times[0], 10, 64)
								if err != nil {
									ss.logger.Errorf("Error while converting time, %v", err)
								}
								at := time.Unix(i, 0)

								message := fmt.Sprintf(">[%s] %s: %s", at, user.Name, result["message"].(string))
								results = append(results, message)
								found = true
							}
						}
					}
					if !found {
						ss.slackAPI.PostMessage(ev.Channel, "Not found", slack.PostMessageParameters{})
						continue
					}

					if len(results) > 10 {
						ss.logger.Info("Uploading result")

						ss.slackAPI.UploadFile(slack.FileUploadParameters{
							Content:  strings.Join(results, "\n"),
							Channels: []string{ev.Channel},
						})
					} else {
						for _, message := range results {

							ss.logger.Infof("Sending message %s to channel %s", message, ev.Channel)
							ss.slackAPI.PostMessage(
								ev.Channel,
								message,
								slack.PostMessageParameters{},
							)
						}
					}
				}
				if ev.Msg.User == ss.me {
					continue
					ss.logger.Info("Skipping mine post")
				}
				ss.logger.Infof("Message %s from channel %s from user %s at %s", ev.Msg.Text, ev.Channel, ev.Msg.User, ev.Msg.Timestamp)
				ss.search.IndexMessage(IndexData{
					ID:        fmt.Sprintf("%s-%s", ev.Msg.User, ev.Msg.Timestamp),
					Username:  ev.Msg.User,
					Message:   ev.Msg.Text,
					Channel:   ev.Channel,
					Timestamp: ev.Timestamp,
				})

			case *slack.LatencyReport:
				ss.logger.Infof("Current latency: %v\n", ev.Value)

			case *slack.RTMError:
				ss.logger.Infof("Error: %s\n", ev.Error())

			case *slack.InvalidAuthEvent:
				ss.logger.Infof("Invalid credentials")
				break

			default:
				continue
			}
		}
	}
	return nil
}

func (ss *SlackService) isToMe(message string) bool {
	return strings.Contains(message, fmt.Sprintf("<@%s>", ss.me))
}

func (ss *SlackService) cleanMessage(message string) string {
	return strings.Replace(message, fmt.Sprintf("<@%s> ", ss.me), "", -1)
}
