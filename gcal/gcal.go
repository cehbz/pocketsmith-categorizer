// Package gcal reads the user's Google Calendar for the trips layer.
package gcal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

// BaseURL is the Calendar API v3.
const BaseURL = "https://www.googleapis.com/calendar/v3"

const pageSize = 2500

// Client reads one calendar's events through an HTTP client that authorizes its requests.
type Client struct {
	BaseURL string
	ID      string // the calendar's id
	HTTP    *http.Client
}

type event struct {
	Summary  string `json:"summary"`
	Location string `json:"location"`
	Start    when   `json:"start"`
	End      when   `json:"end"`
}

// when is an event's start or end: a date for an all-day event, else a time and the zone the
// event keeps it in.
type when struct {
	Date     string `json:"date"`
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// Events lists the calendar's events from from to to, each recurring event's instances one
// by one, a page at a time. The window is read a day wider on each side, so every event with
// a local date in it is read whatever its time zone.
func (c *Client) Events(ctx context.Context, from, to time.Time) ([]trips.Event, error) {
	q := url.Values{
		"singleEvents": {"true"}, "orderBy": {"startTime"}, "maxResults": {fmt.Sprint(pageSize)},
		"timeMin": {from.AddDate(0, 0, -1).Format(time.RFC3339)}, "timeMax": {to.AddDate(0, 0, 2).Format(time.RFC3339)},
	}
	var out []trips.Event
	for {
		var page struct {
			Items []event `json:"items"`
			Next  string  `json:"nextPageToken"`
		}
		if err := c.get(ctx, q, &page); err != nil {
			return nil, err
		}
		for _, e := range page.Items {
			ev, err := e.local()
			if err != nil {
				return nil, err
			}
			out = append(out, ev)
		}
		if page.Next == "" {
			return out, nil
		}
		q.Set("pageToken", page.Next)
	}
}

// get reads a page of the calendar's events into out. A failure reports the API's status and
// message, never the request; a rejected token says to run categorize auth.
func (c *Client) get(ctx context.Context, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/calendars/"+url.PathEscape(c.ID)+"/events?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL adds nothing to the token's or the network's error
		}
		return fmt.Errorf("calendar events: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrAuth
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("calendar events: %s: %s", resp.Status, e.Error.Message)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("calendar events: %w", err)
	}
	return nil
}

// local is e on its local dates.
func (e event) local() (trips.Event, error) {
	start, err := e.Start.local()
	if err != nil {
		return trips.Event{}, err
	}
	end, err := e.End.local()
	if err != nil {
		return trips.Event{}, err
	}
	return trips.Event{Title: e.Summary, Location: e.Location, Start: start, End: end, AllDay: e.Start.Date != ""}, nil
}

// local is w's date in the zone the event keeps it in, else in the zone of its offset.
func (w when) local() (time.Time, error) {
	if w.Date != "" {
		return day.Parse(w.Date)
	}
	t, err := time.Parse(time.RFC3339, w.DateTime)
	if err != nil {
		return time.Time{}, fmt.Errorf("calendar event time %q: %w", w.DateTime, err)
	}
	loc := t.Location()
	if w.TimeZone != "" {
		if loc, err = time.LoadLocation(w.TimeZone); err != nil {
			return time.Time{}, fmt.Errorf("calendar event time zone: %w", err)
		}
	}
	return day.Of(t, loc), nil
}
