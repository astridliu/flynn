package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/api"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/sse"
)

func serveHTTP(host *Host, attach *attachHandler, vman *volume.Manager, sh *shutdown.Handler) (*httprouter.Router, error) {
	l, err := net.Listen("tcp", ":1113")
	if err != nil {
		return nil, err
	}
	sh.BeforeExit(func() { l.Close() })

	r := httprouter.New()

	// host core api
	r.POST("/attach", attach.ServeHTTP)
	r.GET("/host/jobs", hostMiddleware(host, listJobs))
	r.GET("/host/jobs/:id", hostMiddleware(host, getJob))
	r.DELETE("/host/jobs/:id", hostMiddleware(host, stopJob))

	// host volumes api
	r.POST("/volume/provider", volumeMiddleware(vman, volumeapi.CreateProvider))
	r.POST("/volume/provider/:provider_id/newVolume", volumeMiddleware(vman, volumeapi.Create))
	r.PUT("/volume/x/:id/snapshot", volumeMiddleware(vman, volumeapi.Snapshot))
	//r.GET("/volume/x/:id/inspect", volumeMiddleware(vman, volumeapi.Inspect)) // very TODO

	go http.Serve(l, r)

	return r, nil
}

type Host struct {
	state   *State
	backend Backend
}

func (h *Host) StopJob(id string) error {
	job := h.state.GetJob(id)
	if job == nil {
		return errors.New("host: unknown job")
	}
	switch job.Status {
	case host.StatusStarting:
		h.state.SetForceStop(id)
		return nil
	case host.StatusRunning:
		return h.backend.Stop(id)
	default:
		return errors.New("host: job is already stopped")
	}
}

func (h *Host) streamEvents(id string, w http.ResponseWriter) error {
	ch := h.state.AddListener(id)
	go func() {
		<-w.(http.CloseNotifier).CloseNotify()
		h.state.RemoveListener(id, ch)
	}()
	enc := json.NewEncoder(sse.NewSSEWriter(w))
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.WriteHeader(200)
	w.(http.Flusher).Flush()
	for {
		select {
		case data := <-ch:
			if err := enc.Encode(data); err != nil {
				return err
			}
			w.(http.Flusher).Flush()
		case <-time.NewTimer(10 * time.Second).C:
			// if the job still doesn't exist after a reasonable timeout, then
			// break out of this stream (closing the connection will be handled outside)
			if id != "all" && h.state.GetJob(id) == nil {
				return fmt.Errorf("no such job")
			}
		}
	}
	return nil
}

type HostHandle func(*Host, http.ResponseWriter, *http.Request, httprouter.Params)

// Helper function for wrapping a ClusterHandle into a httprouter.Handles
func hostMiddleware(host *Host, handle HostHandle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		handle(host, w, r, ps)
	}
}

type volumeHandle func(*volume.Manager, http.ResponseWriter, *http.Request, httprouter.Params)

// Helper function for wrapping a volumeHandle into a httprouter.Handles
func volumeMiddleware(vman *volume.Manager, handle volumeHandle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		handle(vman, w, r, ps)
	}
}

func listJobs(h *Host, w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	rh := httphelper.NewReponseHelper(w)
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		if err := h.streamEvents("all", w); err != nil {
			rh.Error(err)
		}
		return
	}
	res := h.state.Get()

	rh.JSON(200, res)
}

func getJob(h *Host, w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	rh := httphelper.NewReponseHelper(w)
	id := ps.ByName("id")

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		if err := h.streamEvents(id, w); err != nil {
			rh.Error(err)
		}
		return
	}
	job := h.state.GetJob(id)
	rh.JSON(200, job)
}

func stopJob(h *Host, w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	rh := httphelper.NewReponseHelper(w)
	id := ps.ByName("id")
	if err := h.StopJob(id); err != nil {
		rh.Error(err)
		return
	}
	rh.WriteHeader(200)
}
