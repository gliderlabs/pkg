package main

import (
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"time"

	"github.com/gliderlabs/pkg/usage"
	"github.com/google/go-github/github"
	"github.com/inconshreveable/go-keen"
	"github.com/miekg/dns"
)

type KeenEventTracker interface {
	AddEvent(string, interface{}) error
}

type UsageTracker struct {
	keenClient    KeenEventTracker
	githubClient  *github.Client
	githubProject string
}

func (t *UsageTracker) Track(pv *usage.ProjectVersion) error {
	return t.keenClient.AddEvent("usage", pv)
}

func (t *UsageTracker) GetLatest(pv *usage.ProjectVersion) (*usage.ProjectVersion, error) {
	release, _, err := t.githubClient.Repositories.GetLatestRelease(t.githubProject, pv.Project)
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
	return &usage.ProjectVersion{pv.Project, *release.TagName}, nil
}

func (t *UsageTracker) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)

	q := r.Question[0].Name

	pv, err := usage.ParseV1(q)
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

	m.Answer = append(m.Answer, PtrRecord(latest))

	err = w.WriteMsg(m)
	if err != nil {
		log.Println(err)
	}
}

func PtrRecord(pv *usage.ProjectVersion) *dns.PTR {
	latest := usage.FormatV1(&usage.ProjectVersion{pv.Project, "latest"})
	rr := new(dns.PTR)
	rr.Hdr = dns.RR_Header{Name: latest, Rrtype: dns.TypePTR, Ttl: 0}
	rr.Ptr = usage.FormatV1(pv)
	return rr
}

var keenFlushInterval = flag.Duration("flush", 1*time.Second, "Flush interval for Keen.io")

func main() {
	keenProject := os.Getenv("KEEN_PROJECT")
	keenWriteKey := os.Getenv("KEEN_WRITE_KEY")
	githubProject := os.Getenv("GITHUB_PROJECT")

	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "53"
	}

	addr := net.JoinHostPort(host, port)

	if keenProject == "" || keenWriteKey == "" || githubProject == "" {
		log.Fatal("Please set KEEN_PROJECT, KEEN_WRITE_KEY, and GITHUB_PROJECT")
	}

	keenClient := &keen.Client{WriteKey: keenWriteKey, ProjectID: keenProject}
	keenBatchClient := keen.NewBatchClient(keenClient, *keenFlushInterval)

	githubClient := github.NewClient(nil)

	tracker := UsageTracker{
		keenClient:    keenBatchClient,
		githubClient:  githubClient,
		githubProject: githubProject,
	}

	err := dns.ListenAndServe(addr, "udp", &tracker)
	if err != nil {
		log.Fatal(err)
	}
}
