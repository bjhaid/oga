package requester

import (
	"flag"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/bjhaid/oga/initializer"
	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	"github.com/hashicorp/golang-lru"
	"github.com/nlopes/slack"
)

var (
	token   string
	botName string
	re      *regexp.Regexp
)

const (
	authorName   = "Images to be deployed are:"
	color        = "#1e4ddb"
	textTemplate = "%s Deployment *%s* requires approval, to approve respond " +
		"with:\n\n\t`oga approve %s`"
)

type Annotation struct {
	Slack Config `json:"slack"`
}

type Config struct {
	Channel   string   `json:"channel"`
	Approvers []string `json:"approvers"`
}

type SlackClient interface {
	GetChannels(bool) ([]slack.Channel, error)
	GetUsers() ([]slack.User, error)
	PostMessage(string, string, slack.PostMessageParameters) (string, string,
		error)
	NewRTM() *slack.RTM
	GetUserIdentity() (*slack.UserIdentityResponse, error)
}

type SlackRequester struct {
	Name         string
	api          SlackClient
	requestStore map[string]*initializer.Approval
	userID       string
	initializer  initializer.Interface
	approvers    map[string][]string
	userCache    *lru.Cache
	channelCache *lru.Cache
}

type Logger struct{}

func (logger *Logger) Write(p []byte) (int, error) {
	glog.V(4).Infof(string(p))
	return len(p), nil
}

func init() {
	re = regexp.MustCompile(`oga approve (\w+)`)
	flag.StringVar(&token, "slack-token", "", "Slack API token")
	flag.StringVar(&botName, "bot-name", "", "The username of the oga bot as "+
		"created in slack")
}

func NewSlackRequester() *SlackRequester {
	if token == "" {
		glog.Fatalf("Please provide a slack token via the -slack-token flag")
	}
	if botName == "" {
		glog.Fatalf("Please provide a botname via the -bot-name flag")
	}

	api := slack.New(token)
	api.SetDebug(true)
	slack.SetLogger(log.New(&Logger{}, "", log.Lshortfile))
	userCache, err := lru.New(256)
	if err != nil {
		glog.Fatalf("%s\n", err)
	}
	channelCache, err := lru.New(256)
	if err != nil {
		glog.Fatalf("%s\n", err)
	}

	req := &SlackRequester{
		Name:         "slack",
		api:          api,
		requestStore: make(map[string]*initializer.Approval),
		userCache:    userCache,
		channelCache: channelCache,
		approvers:    make(map[string][]string),
	}

	userID, err := req.getUserID(botName)

	if err != nil {
		glog.Fatalf("%s\n", err)
	}

	req.userID = userID

	return req
}

func (req *SlackRequester) Run() {
	rtm := req.api.NewRTM()
	go rtm.ManageConnection()

	for msg := range rtm.IncomingEvents {
		glog.V(7).Infof("Event Received: %v", msg)
		approverMatches := false
		switch ev := msg.Data.(type) {
		case *slack.MessageEvent:
			if ev.User == req.userID {
				glog.V(3).Infof("Ignoring %v, since it was sent by me", ev)
				continue
			}
			if ev.User == "" {
				glog.V(3).Infof("Ignoring %v, it has no user", ev)
				continue
			}
			match := re.FindStringSubmatch(ev.Text)
			if len(match) == 2 {
				glog.V(3).Infof("Message '%s' matched an approval request", ev.Text)
				var appr string
				var approver string
				for _, approver := range req.approvers[match[1]] {
					appr, _ = req.getUserID(approver)
					if ev.User == appr {
						approverMatches = true
						break
					}
				}
				if approverMatches {
					approval := req.requestStore[match[1]]
					if u, ok := req.userCache.Get(match[1]); ok {
						user := u.(slack.User)
						approval.Approver = fmt.Sprintf(
							"Username: %s, Fullname: %s, Email: %s\n",
							user.Profile.DisplayName, user.Profile.RealName,
							user.Profile.Email)
					} else {
						approval.Approver = approver
					}
					glog.V(3).Info("%s approved %s", approval.Approver, match[1])
					req.initializer.ApproveDeployment(approval)
					message := fmt.Sprintf("<@%s> %s has been approved", ev.User, match[1])
					rtm.SendMessage(rtm.NewOutgoingMessage(message, ev.Channel))
					delete(req.requestStore, match[1])
					delete(req.approvers, match[1])
					approverMatches = false
				} else {
					message := fmt.Sprintf("<@%s> you are not configured to approve *%s*",
						ev.User, match[1])
					rtm.SendMessage(rtm.NewOutgoingMessage(message, ev.Channel))
					continue
				}

			} else {
				glog.V(3).Infof("Message '%s' did not match an approval request",
					ev.Text)
				continue
			}
		case *slack.RTMError:
			glog.Errorf("Error: %s\n", ev.Error())
		case *slack.InvalidAuthEvent:
			glog.Fatalf("Invalid slack credentials")
			return
		default:
			// Ignore other events..
		}
	}
}

//
// annotation should be of the form:
// ```
// approvers:
//   - "@foo"
// 	 - "@bar"
// channel: "#baz"
// ```
func (req *SlackRequester) RequestApproval(oga initializer.Interface,
	app *initializer.Approval, annon string) {
	req.requestStore[app.DeploymentName] = app
	annotation := &Annotation{}
	if err := yaml.Unmarshal([]byte(annon), &annotation); err != nil {
		glog.Errorf("%s\n", err)
		oga.ApproveDeployment(app)
		return
	}

	if annotation.Slack.Channel == "" {
		glog.Errorf("%s does not contain a channel", annon)
		oga.ApproveDeployment(app)
		return
	}
	channel, err := req.getChannelID(annotation.Slack.Channel)
	if err != nil {
		glog.Errorf("%s\n", err)
		oga.ApproveDeployment(app)
		return
	}
	approverIds, err := req.retrieveApproverIds(annotation.Slack.Approvers)
	if err != nil {
		glog.Errorf("%s", err)
		oga.ApproveDeployment(app)
		return
	}

	quotedApproverIds := make([]string, len(approverIds))
	for i, approver := range approverIds {
		quotedApproverIds[i] = "<@" + approver + ">"
	}
	formatedApproverIds := strings.Join(quotedApproverIds, ", ")
	fields :=
		req.convertDeploymentToFields(oga, app.DeploymentName)
	params := slack.PostMessageParameters{}
	attachment := slack.Attachment{
		AuthorName: authorName,
		Color:      color,
		Fields:     fields,
	}
	params.Attachments = []slack.Attachment{attachment}

	text := fmt.Sprintf(textTemplate, formatedApproverIds, app.DeploymentName,
		app.DeploymentName)
	glog.V(3).Infof("Posting %v to slack channel: %s", params, channel)
	_, _, err = req.api.PostMessage(channel, text, params)
	if err != nil {
		glog.Errorf("Posting message to slack failed with: %s", err)
		oga.ApproveDeployment(app)
		return
	}
	req.requestStore[app.DeploymentName] = app
	req.initializer = oga
	req.approvers[app.DeploymentName] =
		annotation.Slack.Approvers
}

func (req *SlackRequester) GetName() string {
	return req.Name
}

func (req *SlackRequester) convertDeploymentToFields(oga initializer.Interface,
	deploymentName string) []slack.AttachmentField {
	deployment := oga.GetDeployment(deploymentName)
	containers := deployment.Spec.Template.Spec.Containers
	fields := make([]slack.AttachmentField, len(containers))

	for i, container := range containers {
		fields[i] = slack.AttachmentField{Value: container.Image, Short: false}
	}

	return fields
}

func (req *SlackRequester) retrieveApproverIds(
	approvers []string) ([]string, error) {
	ids := []string{}
	var err error
	for _, appr := range approvers {
		if user, err := req.getUserID(appr); err != nil {
			glog.Errorf("%s", err)
		} else {
			ids = append(ids, user)
		}
	}

	return ids, err
}

func (req *SlackRequester) getUserID(user string) (string, error) {
	channels, err := req.api.GetUsers()
	if err != nil {
		glog.Errorf("Failed retrieving user ID from the slack API due to: %s",
			err)
		return "", err
	}

	user = strings.Replace(user, "@", "", 1)
	us, ok := req.userCache.Get(user)
	if ok {
		u := us.(slack.User)
		return u.ID, nil
	}
	for _, u := range channels {
		if user == u.Profile.DisplayName || user == u.Profile.RealName {
			req.userCache.Add(user, u)
			return u.ID, nil
		}
	}
	err = fmt.Errorf("%s user does not exist in slack API", user)
	return "", err
}

func (req *SlackRequester) getChannelID(channel string) (string, error) {
	channels, err := req.api.GetChannels(false)
	if err != nil {
		glog.Errorf("Failed retrieving channel ID from the slack API due to: %s",
			err)
		return "", err
	}

	channel = strings.Replace(channel, "#", "", 1)
	chann, ok := req.channelCache.Get(channel)
	if ok {
		cha := chann.(slack.Channel)
		return cha.ID, nil
	}
	for _, cha := range channels {
		if channel == cha.Name {
			req.channelCache.Add(channel, cha)
			return cha.ID, nil
		}
	}
	err = fmt.Errorf("%s channel does not exist in slack API", channel)
	return "", err
}
