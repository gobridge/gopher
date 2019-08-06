package gotime

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"cloud.google.com/go/trace"
)

type GoTime struct {
	http              *http.Client
	notify            func() bool
	startTimeVariance time.Duration

	lastNotified time.Time
}

// New constructs a *GoTime.
//
// startTimeVariance sets the window around the stream's start time when
// a live steam will be considered a GoTime live stream. This is necessary
// because the current changelog APIs return whether any show is streaming
// rather thahn GoTime specifically.
//
// notify is called when streaming starts. notify should return true when a successful.
func New(c *http.Client, startTimeVariance time.Duration, notify func() bool) *GoTime {
	return &GoTime{
		http:              c,
		notify:            notify,
		startTimeVariance: startTimeVariance,
	}
}

// Poll conditionally calls notify if GoTime is currently streaming.
//
// For a notification to be posted: a notification must not have been successful
// in the last 24 hours, changelog is currently streaming, and there is
// a GoTime episode scheduled within +/-startTimeVariance.
func (gt *GoTime) Poll(ctx context.Context) error {
	span := trace.FromContext(ctx).NewChild("GoTime.Poll")
	defer span.Finish()

	now := time.Now()
	if gt.lastNotified.After(now.Add(-24 * time.Hour)) {
		return nil
	}

	var status struct {
		Streaming bool
	}
	err := gt.get(ctx, span, "https://changelog.com/live/status", &status)
	if err != nil {
		return err
	}

	if !status.Streaming {
		return nil
	}

	var countdown struct {
		Data time.Time
	}
	err = gt.get(ctx, span, "https://changelog.com/slack/countdown/gotime", &countdown)
	if err != nil {
		return err
	}

	nextScheduled := countdown.Data
	if now.Before(nextScheduled.Add(-gt.startTimeVariance)) || now.After(nextScheduled.Add(gt.startTimeVariance)) {
		return nil
	}

	if gt.notify() {
		gt.lastNotified = now
	}

	return nil
}

// get makes an HTTP request to url and unmarshals the JSON response into i.
func (gt *GoTime) get(ctx context.Context, span *trace.Span, url string, i interface{}) error {
	childSpan := span.NewChild("GoTime.get")
	defer childSpan.Finish()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	req = req.WithContext(ctx)

	resp, err := gt.http.Do(req)
	if err != nil {
		return fmt.Errorf("making http request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non-200 status code: %d - %s", resp.StatusCode, resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %v", err)
	}

	err = json.Unmarshal(body, i)
	if err != nil {
		return fmt.Errorf("unmarshaling response: %s", err)
	}

	return nil
}
