package bmc

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type TimeRestoreFunc func()

type bmcTimeSnapshot struct {
	ManagerPath string
	DateTime    time.Time
	LocalStart  time.Time
}

func BeginSupermicroTimeWorkaround(c *Client) (TimeRestoreFunc, error) {
	snapshot, err := readRedfishBMCTime(c)
	if err != nil {
		return nil, fmt.Errorf("Supermicro BMC time workaround requires writable Redfish Manager DateTime: %w", err)
	}
	target := safeATENCertificateTime(snapshot.DateTime)
	fmt.Println("Supermicro iKVM time workaround: original BMC time:", snapshot.DateTime.Format(time.RFC3339))
	fmt.Println("Supermicro iKVM time workaround: setting BMC time to:", target.Format(time.RFC3339))
	if err := setAndVerifyRedfishBMCTime(c, snapshot.ManagerPath, target, true); err != nil {
		return nil, err
	}
	return func() {
		restore := snapshot.DateTime.Add(time.Since(snapshot.LocalStart))
		fmt.Println("Supermicro iKVM time workaround: restoring BMC time to:", restore.Format(time.RFC3339))
		if err := setAndVerifyRedfishBMCTime(c, snapshot.ManagerPath, restore, false); err != nil {
			fmt.Println("Supermicro iKVM time workaround: restore was not confirmed:", err)
		}
	}, nil
}

func readRedfishBMCTime(c *Client) (*bmcTimeSnapshot, error) {
	managerPath, err := firstRedfishManagerPath(c)
	if err != nil {
		return nil, err
	}
	body, err := redfishGet(c, managerPath)
	if err != nil {
		return nil, err
	}
	var manager struct {
		DateTime            string `json:"DateTime"`
		DateTimeLocalOffset string `json:"DateTimeLocalOffset"`
	}
	if err := json.Unmarshal(body, &manager); err != nil {
		return nil, err
	}
	bmcTime, err := parseRedfishTime(manager.DateTime, manager.DateTimeLocalOffset)
	if err != nil {
		return nil, fmt.Errorf("%s DateTime %q: %w", managerPath, manager.DateTime, err)
	}
	return &bmcTimeSnapshot{ManagerPath: managerPath, DateTime: bmcTime, LocalStart: time.Now()}, nil
}

func firstRedfishManagerPath(c *Client) (string, error) {
	body, err := redfishGet(c, "/redfish/v1/Managers")
	if err != nil {
		return "", err
	}
	var collection struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	if err := json.Unmarshal(body, &collection); err != nil {
		return "", err
	}
	for _, member := range collection.Members {
		if member.ODataID != "" {
			return member.ODataID, nil
		}
	}
	return "", fmt.Errorf("no Redfish Managers members found")
}

func setAndVerifyRedfishBMCTime(c *Client, managerPath string, value time.Time, rollbackCheck bool) error {
	payload, _ := json.Marshal(map[string]string{"DateTime": value.Format(time.RFC3339)})
	_, _, _, err := c.PatchRawResponse(managerPath, string(payload), http.Header{
		"Accept":       {"application/json"},
		"Content-Type": {"application/json"},
	})
	if err != nil {
		return fmt.Errorf("set Redfish DateTime: %w", err)
	}
	deadline := time.Now().Add(12 * time.Second)
	var last time.Time
	var lastErr error
	for time.Now().Before(deadline) {
		time.Sleep(700 * time.Millisecond)
		snapshot, err := readRedfishBMCTime(c)
		if err != nil {
			lastErr = err
			continue
		}
		last = snapshot.DateTime
		if rollbackCheck && isInsideATENCertificateWindow(last) {
			fmt.Println("Supermicro iKVM time workaround: BMC rollback confirmed:", last.Format(time.RFC3339))
			return nil
		}
		if !rollbackCheck && absDuration(last.Sub(value)) <= 3*time.Minute {
			fmt.Println("Supermicro iKVM time workaround: BMC restore confirmed:", last.Format(time.RFC3339))
			return nil
		}
	}
	if lastErr != nil {
		return fmt.Errorf("verify Redfish DateTime: %w", lastErr)
	}
	if last.IsZero() {
		return fmt.Errorf("verify Redfish DateTime: no readable value")
	}
	return fmt.Errorf("verify Redfish DateTime: got %s, wanted near %s", last.Format(time.RFC3339), value.Format(time.RFC3339))
}

func redfishGet(c *Client, path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.URL(path), nil)
	if err != nil {
		return nil, err
	}
	c.applyRequestHost(req)
	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func parseRedfishTime(value, offset string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty DateTime")
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if offset != "" && !strings.HasSuffix(value, "Z") && !strings.Contains(value[10:], "+") && !strings.Contains(value[10:], "-") {
		if t, err := time.Parse(time.RFC3339, value+offset); err == nil {
			return t, nil
		}
	}
	if t, err := time.Parse("2006-01-02T15:04:05", value); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unsupported DateTime format")
}

func safeATENCertificateTime(original time.Time) time.Time {
	location := original.Location()
	if location == nil {
		location = time.UTC
	}
	return time.Date(2025, 1, 15, 12, 0, 0, 0, location)
}

func isInsideATENCertificateWindow(value time.Time) bool {
	notBefore := time.Date(2016, 5, 20, 0, 0, 0, 0, value.Location())
	notAfter := time.Date(2026, 5, 16, 0, 0, 0, 0, value.Location())
	return value.After(notBefore) && value.Before(notAfter)
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
