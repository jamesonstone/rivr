package maintenance

import "encoding/json"

func jsonPullRequests(requests []PullRequest) ([]byte, error) {
	return json.Marshal(requests)
}
