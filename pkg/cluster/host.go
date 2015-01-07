package cluster

import (
	"bufio"
	"fmt"
	"io"
	"net/http"

	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/httpclient"
	"github.com/flynn/flynn/pkg/sse"
)

// Host is a client for a host daemon.
type Host interface {
	// ListJobs lists the jobs running on the host.
	ListJobs() (map[string]host.ActiveJob, error)

	// GetJob retrieves job details by ID.
	GetJob(id string) (*host.ActiveJob, error)

	// StopJob stops a running job.
	StopJob(id string) error

	// StreamEvents about job state changes to ch. id may be "all" or a single
	// job ID.
	StreamEvents(id string, ch chan<- *host.Event) (Stream, error)

	// Attach attaches to a job, optionally waiting for it to start before
	// attaching.
	Attach(req *host.AttachReq, wait bool) (AttachClient, error)

	// Close frees the underlying connection to the host.
	Close() error
}

type hostClient struct {
	c *httpclient.Client
}

// NewHostClient creates a new Host that uses client to communicate with it.
// addr is used by Attach.
func NewHostClient(addr string, h *http.Client) Host {
	if h == nil {
		h = http.DefaultClient
	}
	return &hostClient{c: &httpclient.Client{
		ErrPrefix:   "host",
		ErrNotFound: ErrNotFound,
		URL:         addr,
		HTTP:        h,
	}}
}

func (c *hostClient) ListJobs() (map[string]host.ActiveJob, error) {
	var jobs map[string]host.ActiveJob
	err := c.c.Get("/host/jobs", &jobs)
	return jobs, err
}

func (c *hostClient) GetJob(id string) (*host.ActiveJob, error) {
	var res host.ActiveJob
	err := c.c.Get(fmt.Sprintf("/host/jobs/%s", id), &res)
	return &res, err
}

func (c *hostClient) StopJob(id string) error {
	return c.c.Delete(fmt.Sprintf("/host/jobs/%s", id))
}

func (c *hostClient) StreamEvents(id string, ch chan<- *host.Event) (Stream, error) {
	header := http.Header{"Accept": []string{"text/event-stream"}}
	r := fmt.Sprintf("/host/jobs/%s", id)
	if id == "all" {
		r = "/host/jobs"
	}
	res, err := c.c.RawReq("GET", r, header, nil, nil)

	if err != nil {
		return nil, err
	}

	stream := EventStream{
		Chan: ch,
		body: res.Body,
	}
	go func() {
		defer func() {
			close(ch)
			stream.Close()
		}()

		r := bufio.NewReader(stream.body)
		dec := sse.NewDecoder(r)
		for {
			event := &host.Event{}
			if err := dec.Decode(event); err != nil {
				if err != io.EOF {
					stream.err = err
				}
				break
			}
			stream.Chan <- event
		}
	}()
	return stream, nil
}

func (c *hostClient) Close() error {
	return c.c.Close()
}

type EventStream struct {
	Chan chan<- *host.Event
	body io.ReadCloser
	err  error
}

func (e EventStream) Err() error {
	return e.err
}

func (e EventStream) Close() error {
	return e.body.Close()
}
