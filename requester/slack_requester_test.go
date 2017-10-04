package requester

import (
	"encoding/json"
	"testing"

	"github.com/bjhaid/oga/initializer"
	"github.com/ghodss/yaml"
	"github.com/hashicorp/golang-lru"
	"github.com/nlopes/slack"

	"k8s.io/client-go/pkg/apis/apps/v1beta1"
)

type FakeInitializer struct{}

func (oga *FakeInitializer) GetDeployment(depName string) *v1beta1.Deployment {
	return initializer.NewDeployment(depName)
}

type FakeSlackClient struct{}

type conversation struct {
	ID string
}

type groupConversation struct {
	conversation
	Name string
}

func (api *FakeSlackClient) GetUsers() ([]slack.User, error) {
	dat := `[
        {
            "id": "U7B832F2P",
            "team_id": "T7BAAC4G2",
            "name": "ayo",
            "profile": {
                "display_name": "bjhaid",
                "real_name": "bjhaid",
                "team": "T7BAAC4G2"
            }
        },
        {
            "id": "U7B832F2P",
            "team_id": "T7BAAC4G2",
            "name": "foobar",
            "profile": {
                "display_name": "foo",
                "real_name": "foo",
                "team": "T7BAAC4G2"
            }
        }
    ]`
	users := make([]slack.User, 0, 2)
	json.Unmarshal([]byte(dat), &users)
	return users, nil
}

func (api *FakeSlackClient) PostMessage(channel string, text string,
	params slack.PostMessageParameters) (string, string, error) {
	return "", "", nil
}

func (api *FakeSlackClient) NewRTM() *slack.RTM {
	return &slack.RTM{}
}

func (api *FakeSlackClient) GetUserIdentity() (*slack.UserIdentityResponse, error) {
	return &slack.UserIdentityResponse{}, nil
}

func (api *FakeSlackClient) GetChannels(_ bool) ([]slack.Channel, error) {
	channel := slack.Channel{}
	j := `{
            "id": "C7ALL3GP2",
            "name": "bar",
						"is_channel": true,
						"is_member": true,
						"is_general": true
        }`
	json.Unmarshal([]byte(j), &channel)
	channel1 := slack.Channel{}
	j = `{
            "id": "C7CCVMSTZ",
            "name": "foo",
						"is_channel": true,
						"is_member": true,
						"is_general": true
        }`
	json.Unmarshal([]byte(j), &channel1)
	return []slack.Channel{channel, channel1}, nil
}

func (oga *FakeInitializer) ApproveDeployment(
	approvedDeployment *initializer.Approval) {
}

func TestRetrieveApprovers(t *testing.T) {
	userCache, _ := lru.New(256)
	channelCache, _ := lru.New(256)
	req := &SlackRequester{
		Name: "slack", api: &FakeSlackClient{},
		userCache: userCache, channelCache: channelCache}
	annon := "slack:\n" +
		"  channel: \"#bar\"\n" +
		"  approvers: \n" +
		"    - \"@bjhaid\"\n" +
		"    - \"@foo\"\n"
	annotation := Annotation{}
	err := yaml.Unmarshal([]byte(annon), &annotation)
	approvers, err := req.retrieveApproverIds(annotation.Slack.Approvers)
	expectedApprovers := []string{"U7B832F2P", "U7B832F2P"}

	if err != nil {
		t.Errorf("%s\n", err)
	}

	for i, approver := range approvers {
		if expectedApprovers[i] != approver {
			t.Errorf("Expected approver: %s, got %s\n", expectedApprovers[i],
				approver)
		}
	}

	annon = "slack:\n" +
		"  channel: \"#bar\"\n"
	annotation = Annotation{}
	yaml.Unmarshal([]byte(annon), &annotation)
	approvers, err = req.retrieveApproverIds(annotation.Slack.Approvers)

	if len(approvers) != 0 {
		t.Errorf("Expected '0' approvers got: '%d'\n", len(approvers))
	}
}

func TestGetChannelId(t *testing.T) {
	annon := "slack:\n" +
		"  channel: \"#bar\"\n" +
		"  approvers: \n" +
		"    - \"@bjhaid\"\n" +
		"    - \"@foo\"\n"
	userCache, _ := lru.New(256)
	channelCache, _ := lru.New(256)
	req := &SlackRequester{
		Name: "slack", api: &FakeSlackClient{},
		userCache: userCache, channelCache: channelCache}
	annotation := Annotation{}
	err := yaml.Unmarshal([]byte(annon), &annotation)
	channel, err := req.getChannelID(annotation.Slack.Channel)

	if err != nil {
		t.Errorf("%s\n", err)
	}

	if channel != "C7ALL3GP2" {
		t.Errorf("Expected channel: 'C7ALL3GP2', got %s\n", channel)
	}

	annon = `
slack:
  channel:
`
	annotation = Annotation{}
	if err = yaml.Unmarshal([]byte(annon), &annotation); err != nil {
		t.Errorf("%s\n", err)
	}

	if _, err = req.getChannelID(annotation.Slack.Channel); err == nil {
		t.Errorf("Expected getChannelID to return error but didn't")
	}

}

func TestConvertDeploymentToFields(t *testing.T) {
	userCache, _ := lru.New(256)
	channelCache, _ := lru.New(256)
	req := &SlackRequester{
		Name: "slack", api: &FakeSlackClient{},
		userCache: userCache, channelCache: channelCache}
	fields := req.convertDeploymentToFields(&FakeInitializer{}, "foo")

	expected := []slack.AttachmentField{
		slack.AttachmentField{Value: "foo/bar", Short: false},
		slack.AttachmentField{Value: "bar/baz", Short: false},
	}

	if len(fields) != 2 {
		t.Errorf("Expected 2 elements in fields got: %d\n", len(fields))
	}

	for i, field := range fields {
		if expected[i].Value != field.Value {
			t.Errorf("Expected field %v, got: %v\n", expected[i].Value, field.Value)
		}
	}
}

func TestRequestApproval(t *testing.T) {
	userCache, _ := lru.New(256)
	channelCache, _ := lru.New(256)
	req := &SlackRequester{
		Name: "slack", api: &FakeSlackClient{},
		userCache: userCache, channelCache: channelCache,
		requestStore: make(map[string]*initializer.Approval),
		approvers:    make(map[string][]string),
	}
	annon := "slack:\n" +
		"  channel: \"#bar\"\n" +
		"  approvers: \n" +
		"    - \"@bjhaid\"\n" +
		"    - \"@foo\"\n"
	req.RequestApproval(
		&FakeInitializer{},
		&initializer.Approval{DeploymentName: "foo", RequesterName: "slack"},
		annon)

	if req.requestStore["foo"] == nil {
		t.Errorf("Expected approval request with deploymentname foo stored")
	}
	if req.initializer == nil {
		t.Errorf("Expected initiliazer stored but missing")
	}
	if len(req.approvers["foo"]) != 2 {
		t.Errorf("Expected 2 approvers got: %d\n", len(req.approvers))
	}
	expectedApprovers := []string{"@bjhaid", "@foo"}
	for i, approver := range req.approvers["foo"] {
		if expectedApprovers[i] != approver {
			t.Errorf("Expected %s got: %s\n",
				expectedApprovers[i], approver)
		}
	}
}
