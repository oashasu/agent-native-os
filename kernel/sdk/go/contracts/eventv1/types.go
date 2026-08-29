package eventv1

import (
	appendv1 "github.com/example/agent-native-microkernel/sdk/go/generated/eventjournalappendv1"
	replayv1 "github.com/example/agent-native-microkernel/sdk/go/generated/eventjournalreplayv1"
)

type Record = appendv1.ResponseRecord
type AppendRequest = appendv1.Request
type AppendResponse = appendv1.Response
type ReplayRequest = replayv1.Request
type ReplayResponse struct {
	Records []Record `json:"records"`
	Next    int      `json:"next"`
}

var _ = replayv1.ResponseRecordsItem(Record{})
var _ = Record(replayv1.ResponseRecordsItem{})
