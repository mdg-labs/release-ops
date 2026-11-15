package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Delivery is one attempt to deliver a webhook event, from GET /app/hook/deliveries. The
// payload is not part of the listing and never read.
type Delivery struct {
	ID          int64     `json:"id"`
	GUID        string    `json:"guid"`
	DeliveredAt time.Time `json:"delivered_at"`
	// Redelivery is true for an attempt made by a redelivery of the same GUID.
	Redelivery bool `json:"redelivery"`
	// StatusCode is what the receiver answered; 0 when it did not answer.
	StatusCode int `json:"status_code"`
}

func (d Delivery) valid() bool {
	return d.ID > 0 && d.GUID != "" && !d.DeliveredAt.IsZero()
}

// Succeeded reports that the receiver accepted the delivery with a 2xx status.
func (d Delivery) Succeeded() bool {
	return d.StatusCode >= 200 && d.StatusCode < 300
}

// ListDeliveries lists the webhook deliveries of the App that appJWT authenticates, newest
// first, until it reaches one delivered before since. GitHub lists them most recent first, so
// the listing ends there instead of reading the whole history.
func (c *Client) ListDeliveries(ctx context.Context, appJWT string, since time.Time) ([]Delivery, error) {
	var out []Delivery
	err := c.pages(ctx, "/app/hook/deliveries", appJWT, func(body []byte) error {
		var page []Delivery
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("%w: response is not a list of deliveries", ErrRequestFailed)
		}
		for _, d := range page {
			if !d.valid() {
				return fmt.Errorf("%w: response holds something that is not a delivery", ErrRequestFailed)
			}
			if d.DeliveredAt.Before(since) {
				return errStopPaging
			}
			out = append(out, d)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Redeliver asks GitHub to deliver the delivery again to the webhook URL. The repeat goes
// through the receiver like any other delivery; nothing of it is read here.
func (c *Client) Redeliver(ctx context.Context, appJWT string, deliveryID int64) error {
	status, _, err := c.do(ctx, http.MethodPost, "/app/hook/deliveries/"+strconv.FormatInt(deliveryID, 10)+"/attempts", appJWT, nil)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusAccepted:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrAppRejected
	default:
		return fmt.Errorf("%w: status %d", ErrRequestFailed, status)
	}
}

// Deliveries lists the App's webhook deliveries since the given time, with the App JWT.
func (c *AppClient) Deliveries(ctx context.Context, since time.Time) ([]Delivery, error) {
	jwt, _, err := c.appJWT(ctx)
	if err != nil {
		return nil, err
	}
	return c.api.ListDeliveries(ctx, jwt, since)
}

// Redeliver asks GitHub to redeliver a delivery, with the App JWT.
func (c *AppClient) Redeliver(ctx context.Context, deliveryID int64) error {
	jwt, _, err := c.appJWT(ctx)
	if err != nil {
		return err
	}
	return c.api.Redeliver(ctx, jwt, deliveryID)
}
