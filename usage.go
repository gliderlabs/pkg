package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/go-github/github"
	"github.com/inconshreveable/go-keen"
	"github.com/miekg/dns"
)

func PtrRecord(pv *ProjectVersion) *dns.PTR {
	latest := FormatV1(&ProjectVersion{pv.Project, "latest"})
	rr := new(dns.PTR)
	rr.Hdr = dns.RR_Header{Name: latest, Rrtype: dns.TypePTR, Ttl: 0}
	rr.Ptr = FormatV1(pv)
	return rr
}

func TxtRecord(pv *ProjectVersion) *dns.TXT {
	latest := FormatV1(&ProjectVersion{pv.Project, "latest"})
	rr := new(dns.TXT)
	rr.Hdr = dns.RR_Header{Name: latest, Rrtype: dns.TypeTXT, Ttl: 0}
	rr.Txt = []string{
		"project=" + pv.Project,
		"version=" + pv.Version,
	}
	return rr
}

type ProjectVersion struct {
	Project string
	Version string
	// other client info?
	// IPs or something might be interesting, but privacy concerns?
	// use IP->Geo lookups?
	// https://keen.io/docs/api/?shell#ip-to-geo-parser
	// OS type?
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

type KeenEventTracker interface {
	AddEvent(string, interface{}) error
}

type UsageTracker struct {
	keenClient   KeenEventTracker
	githubClient *github.Client
}

func (t *UsageTracker) Track(pv *ProjectVersion) error {
	return t.keenClient.AddEvent("usage", pv)
}

func (t *UsageTracker) GetLatest(pv *ProjectVersion) (*ProjectVersion, error) {
	release, _, err := t.githubClient.Repositories.GetLatestRelease("gliderlabs", pv.Project)
	if err != nil {
		// TODO look for 404 errors
		// 404 can mean that the project doesn't exist, or it has no releases yet
		// if err, ok := err.(*github.ErrorResponse); ok {
		// 	err.Response.StatusCode == 404
		// }
		return nil, err
	}
	if release.TagName == nil {
		return nil, errors.New("missing TagName")
	}
	return &ProjectVersion{pv.Project, *release.TagName}, nil
}

func (t *UsageTracker) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)

	q := r.Question[0].Name

	pv, err := ParseV1(q)
	if err != nil {
		log.Println(err)
		return
	}

	latest, err := t.GetLatest(pv)
	if err != nil {
		// TODO if format is right, but project is missing,
		// return an NXDOMAIN error
		log.Println(err)
		return
	}

	// do this after getting the version so we don't track results for
	// projects that aren't found
	err = t.Track(pv)
	if err != nil {
		log.Println(err)
		// tracking error is not fatal, so still return the results
	}

	m.Answer = append(
		m.Answer,
		PtrRecord(latest),
		TxtRecord(latest),
	)

	err = w.WriteMsg(m)
	if err != nil {
		log.Println(err)
	}
}

var keenFlushInterval = flag.Duration("flush", 1*time.Second, "Flush interval for Keen.io")

func main() {
	keenProject := os.Getenv("KEEN_PROJECT")
	keenWriteKey := os.Getenv("KEEN_WRITE_KEY")

	if keenProject == "" || keenWriteKey == "" {
		log.Fatal("Please set both KEEN_PROJECT and KEEN_WRITE_KEY")
	}

	keenClient := &keen.Client{WriteKey: keenWriteKey, ProjectID: keenProject}
	keenBatchClient := keen.NewBatchClient(keenClient, *keenFlushInterval)

	githubClient := github.NewClient(nil)

	tracker := UsageTracker{
		keenClient:   keenBatchClient,
		githubClient: githubClient,
	}

	err := dns.ListenAndServe(":5354", "udp", &tracker)
	if err != nil {
		log.Fatal(err)
	}
}
