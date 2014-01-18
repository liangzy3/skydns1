package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/miekg/dns"
	"github.com/skynetservices/skydns/msg"
	"io"
	"net"
	"net/http"
	"strconv"
)

var (
	ErrNoHttpAddress   = errors.New("No HTTP address specified")
	ErrInvalidResponse = errors.New("Invalid HTTP response")
	ErrServiceNotFound = errors.New("Service not found")
	ErrConflictingUUID = errors.New("Conflicting UUID")
)

type (
	Client struct {
		base    string
		secret  string
		h       *http.Client
		basedns string
		domain  string
		d       *dns.Client
	}

	NameCount map[string]int
)

// NewClient creates a new skydns client with the specificed host address and
// dns port.
func NewClient(base, secret, dnsdomain string, dnsport int) (*Client, error) {
	if base == "" {
		return nil, ErrNoHttpAddress
	}
	if dnsport == 0 {
		dnsport = 53
	}
	host, _, err := net.SplitHostPort(base[6:])
	if err != nil {
		// TODO(miek): https?
	}

	return &Client{
		base:    base,
		basedns: net.JoinHostPort(host, strconv.Itoa(dnsport)),
		domain:  "." + dns.Fqdn(dnsdomain),
		secret:  secret,
		h:       &http.Client{},
		d:       &dns.Client{},
	}, nil
}

func (c *Client) Add(uuid string, s *msg.Service) error {
	b := bytes.NewBuffer(nil)
	if err := json.NewEncoder(b).Encode(s); err != nil {
		return err
	}
	req, err := c.newRequest("PUT", c.joinUrl(uuid), b)
	if err != nil {
		return err
	}
	resp, err := c.h.Do(req)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	switch resp.StatusCode {
	case http.StatusCreated:
		return nil
	case http.StatusConflict:
		return ErrConflictingUUID
	default:
		return ErrInvalidResponse
	}
}

func (c *Client) Delete(uuid string) error {
	req, err := c.newRequest("DELETE", c.joinUrl(uuid), nil)
	if err != nil {
		return err
	}
	resp, err := c.h.Do(req)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return nil
}

func (c *Client) Get(uuid string) (*msg.Service, error) {
	req, err := c.newRequest("GET", c.joinUrl(uuid), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.h.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	switch resp.StatusCode {
	case http.StatusOK:
		break
	case http.StatusNotFound:
		return nil, ErrServiceNotFound
	default:
		return nil, ErrInvalidResponse
	}

	var s *msg.Service
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return s, nil
}

func (c *Client) Update(uuid string, ttl uint32) error {
	b := bytes.NewBuffer([]byte(fmt.Sprintf(`{"TTL":%d}`, ttl)))
	req, err := c.newRequest("PATCH", c.joinUrl(uuid), b)
	if err != nil {
		return err
	}
	resp, err := c.h.Do(req)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return nil
}

func (c *Client) GetAllServices() ([]*msg.Service, error) {
	req, err := c.newRequest("GET", c.joinUrl(""), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.h.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	var out []*msg.Service
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetRegions() (NameCount, error) {
	req, err := c.newRequest("GET", fmt.Sprintf("%s/skydns/regions/", c.base), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.h.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	var out NameCount
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetRegionsDNS() (NameCount, error) {
	req, err := c.newRequestDNS("regions", dns.TypeSRV)
	if err != nil {
		return nil, err
	}
	resp, _, err := c.d.Exchange(req, c.basedns)
	if err != nil {
		return nil, err
	}

	var out NameCount
	resp = resp
	return out, nil
}

func (c *Client) GetEnvironments() (NameCount, error) {
	req, err := c.newRequest("GET", fmt.Sprintf("%s/skydns/environments/", c.base), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.h.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	var out NameCount
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) AddCallback(uuid string, cb *msg.Callback) error {
	buf := bytes.NewBuffer(nil)
	if err := json.NewEncoder(buf).Encode(cb); err != nil {
		return err
	}
	req, err := c.newRequest("PUT", fmt.Sprintf("%s/skydns/callbacks/%s", c.base, uuid), buf)
	if err != nil {
		return err
	}
	resp, err := c.h.Do(req)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	switch resp.StatusCode {
	case http.StatusCreated:
		return nil
	case http.StatusNotFound:
		return ErrServiceNotFound
	default:
		return ErrInvalidResponse
	}
}

func (c *Client) joinUrl(uuid string) string {
	return fmt.Sprintf("%s/skydns/services/%s", c.base, uuid)
}

func (c *Client) newRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if c.secret != "" {
		req.Header.Add("Authorization", c.secret)
	}
	return req, err
}

func (c *Client) newRequestDNS(qname string, qtype uint16) (*dns.Msg, error) {
	m := new(dns.Msg)
	m.SetQuestion(qname, qtype)
	return m, nil
}
