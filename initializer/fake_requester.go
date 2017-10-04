package initializer

type FakeRequester struct {
	Name string
}

func (req *FakeRequester) RequestApproval(_ Interface, app *Approval, annon string) {
}

func (req *FakeRequester) GetName() string {
	return req.Name
}
