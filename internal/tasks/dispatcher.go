// Package tasks enqueues background research jobs on Cloud Tasks.
package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Job is the JSON payload posted to /tasks/run-research.
type Job struct {
	LineUserID string `json:"line_user_id"`
	Topic      string `json:"topic"`
	Mode       string `json:"mode"` // new | deepen | retry_write | retry_publish
	ReportID   string `json:"report_id,omitempty"`
	TaskID     string `json:"task_id"`
}

// DispatchDeadline is the max Cloud Tasks allows for HTTP targets. Research
// runs can take several minutes; the default (10m) would cause duplicate
// re-dispatches of slow jobs.
const DispatchDeadline = 30 * time.Minute

type Dispatcher struct {
	client    *cloudtasks.Client
	queuePath string
	targetURL string
	audience  string
	saEmail   string
}

func New(ctx context.Context, projectID, location, queue, targetURL, audience, saEmail string) (*Dispatcher, error) {
	c, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &Dispatcher{
		client:    c,
		queuePath: fmt.Sprintf("projects/%s/locations/%s/queues/%s", projectID, location, queue),
		targetURL: targetURL,
		audience:  audience,
		saEmail:   saEmail,
	}, nil
}

func (d *Dispatcher) Enqueue(ctx context.Context, job Job) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = d.client.CreateTask(ctx, &cloudtaskspb.CreateTaskRequest{
		Parent: d.queuePath,
		Task: &cloudtaskspb.Task{
			DispatchDeadline: durationpb.New(DispatchDeadline),
			MessageType: &cloudtaskspb.Task_HttpRequest{HttpRequest: &cloudtaskspb.HttpRequest{
				HttpMethod: cloudtaskspb.HttpMethod_POST,
				Url:        d.targetURL,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       body,
				AuthorizationHeader: &cloudtaskspb.HttpRequest_OidcToken{OidcToken: &cloudtaskspb.OidcToken{
					ServiceAccountEmail: d.saEmail,
					Audience:            d.audience,
				}},
			}},
		},
	})
	return err
}

func (d *Dispatcher) Close() error { return d.client.Close() }
