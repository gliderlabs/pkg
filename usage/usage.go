package usage

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/miekg/dns"
)

const dnsHost = "usage.gliderlabs.com"
const dnsTimeout = 2 * time.Second

type ProjectVersion struct {
	Project string
	Version string
}

func RequestLatest(pv *ProjectVersion) (*ProjectVersion, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(FormatV1(pv), dns.TypePTR)
	in, err := dns.Exchange(msg, dnsHost)
	if err != nil {
		return nil, err
	}
	for _, ans := range in.Answer {
		if ptr, ok := ans.(*dns.PTR); ok {
			return ParseV1(ptr.Ptr)
		}
	}
	return nil, errors.New("no answer found")
}

func Send(pv *ProjectVersion) error {
	// TODO could even start a go routine for this so it runs in the background
	// without blocking at all for connecting
	co, err := dns.DialTimeout("udp", dnsHost, dnsTimeout)
	if err != nil {
		return err
	}
	defer co.Close()
	co.SetReadDeadline(time.Now().Add(dnsTimeout))
	co.SetWriteDeadline(time.Now().Add(dnsTimeout))

	msg := new(dns.Msg)
	msg.SetQuestion(FormatV1(pv), dns.TypePTR)
	return co.WriteMsg(msg)
}

func ParseV1(domain string) (*ProjectVersion, error) {
	prefix := strings.TrimSuffix(domain, ".usage-v1.")
	if len(prefix) == len(domain) {
		return nil, errors.New("should end in '.usage-v1.'")
	}

	lastDot := strings.LastIndex(prefix, ".")
	if lastDot < 0 {
		return nil, errors.New("missing '.' separator")
	}

	version := prefix[:lastDot]
	project := prefix[lastDot+1:]

	if len(version) == 0 {
		return nil, errors.New("version should not be empty")
	}
	if len(project) == 0 {
		return nil, errors.New("project should not be empty")
	}

	return &ProjectVersion{project, version}, nil
}

func FormatV1(pv *ProjectVersion) string {
	return fmt.Sprintf("%s.%s.usage-v1.", pv.Version, pv.Project)
}
