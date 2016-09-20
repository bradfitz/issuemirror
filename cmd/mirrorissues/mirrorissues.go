// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/build"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bradfitz/issuemirror"
	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

var (
	destPkg = flag.String("dstpkg", "github.com/bradfitz/go-issue-mirror", "package to update with the issues from Github")
	reclean = flag.Bool("reclean", false, "reclean old issues")
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: mirrorissues\n")
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	tokenFile := filepath.Join(os.Getenv("HOME"), "keys", "github-mirror-go-issues")
	slurp, err := ioutil.ReadFile(tokenFile)
	if err != nil {
		log.Fatal(err)
	}
	f := strings.SplitN(strings.TrimSpace(string(slurp)), ":", 2)
	if len(f) != 2 || f[0] == "" || f[1] == "" {
		log.Fatalf("Expected token file %s to be of form <username>:<token>", tokenFile)
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: f[1]})
	tc := oauth2.NewClient(oauth2.NoContext, ts)
	client := github.NewClient(tc)

	if flag.NArg() > 0 {
		usage()
	}

	p, err := build.Import(*destPkg, "", build.FindOnly)
	if err != nil {
		log.Fatal(err)
	}
	destRoot := p.Dir

	root := issuemirror.Root(destRoot)
	page := 1
	keepGoing := true
	for keepGoing {
		issues, res, err := client.Issues.ListByRepo("golang", "go", &github.IssueListByRepoOptions{
			State:     "all",
			Sort:      "updated",
			Direction: "desc",
			ListOptions: github.ListOptions{
				Page:    page,
				PerPage: 100,
			},
		})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("page %d, num issues %d, res: %#v", page, len(issues), res)
		keepGoing = false
		for _, is := range issues {
			cleanIssue(is)
			issueFile := root.IssueJSONFile(*is.Number)
			wrote, err := writeDiskJSON(issueFile, *is.UpdatedAt, is)
			if err != nil {
				log.Fatal(err)
			}
			if wrote {
				keepGoing = true
			}
		}
		page++
	}

	updateComments := func(issueNum int) error {
		page := 1
		for {
			comments, res, err := client.Issues.ListComments("golang", "go", issueNum, &github.IssueListCommentsOptions{
				ListOptions: github.ListOptions{
					Page:    page,
					PerPage: 100,
				},
			})
			if err != nil {
				return err
			}
			for _, c := range comments {
				cleanComment(c)
				_, err := writeDiskJSON(
					root.IssueCommentFile(issueNum, *c.ID),
					*c.UpdatedAt,
					c)
				if err != nil {
					return err
				}
			}
			page = res.NextPage
			log.Printf("next page: %v", page)
			if page == 0 {
				return nil
			}
		}
	}

	if err := root.ForeachIssue(func(is *github.Issue) error {
		if is.Comments == nil {
			return nil
		}

		if *reclean {
			if cleanIssue(is) {
				if _, err := writeDiskJSON(root.IssueJSONFile(*is.Number), time.Time{}, is); err != nil {
					return err
				}
			}
			issueNum := *is.Number
			if err := root.ForeachIssueComment(issueNum, func(c *github.IssueComment) error {
				if cleanComment(c) {
					if _, err := writeDiskJSON(root.IssueCommentFile(issueNum, *c.ID),
						time.Time{}, c); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
		numComments := *is.Comments
		onDisk, err := root.NumComments(*is.Number)
		if err != nil {
			return err
		}

		if onDisk == numComments {
			return nil
		}
		return updateComments(*is.Number)
	}); err != nil {
		log.Fatal(err)
	}
}

func cleanComment(c *github.IssueComment) (cleaned bool) {
	if c == nil {
		return
	}
	if cleanUser(c.User) {
		cleaned = true
	}
	if cleanReactionsPtr(&c.Reactions) {
		cleaned = true
	}
	cleanStr(&c.URL, &cleaned)
	cleanStr(&c.HTMLURL, &cleaned)
	cleanStr(&c.IssueURL, &cleaned)
	return
}

func cleanReactionsPtr(rp **github.Reactions) (cleaned bool) {
	r := *rp
	if r == nil {
		return
	}
	if r.TotalCount == nil || *r.TotalCount == 0 {
		*rp = nil
		cleaned = true
	} else if cleanReactions(r) {
		cleaned = true
	}
	return
}

func cleanIssue(is *github.Issue) (cleaned bool) {
	if cleanUser(is.User) {
		cleaned = true
	}
	cleanStr(&is.URL, &cleaned)
	cleanStr(&is.HTMLURL, &cleaned)
	for i := range is.Labels {
		if cleanLabel(&is.Labels[i]) {
			cleaned = true
		}
	}
	if cleanReactionsPtr(&is.Reactions) {
		cleaned = true
	}
	if cleanUser(is.Assignee) {
		cleaned = true
	}
	if len(is.Assignees) == 1 { // useless
		is.Assignees = nil
		cleaned = true
	}
	for _, u := range is.Assignees {
		if cleanUser(u) {
			cleaned = true
		}
	}
	if cleanIssueMilestone(is.Milestone) {
		cleaned = true
	}
	return
}

func cleanIssueMilestone(m *github.Milestone) (cleaned bool) {
	if m == nil {
		return false
	}
	cleanStr(&m.URL, &cleaned)
	cleanStr(&m.HTMLURL, &cleaned)
	cleanStr(&m.LabelsURL, &cleaned)
	cleanStr(&m.State, &cleaned)
	cleanStr(&m.Description, &cleaned)
	if m.Creator != nil {
		m.Creator = nil
		cleaned = true
	}
	if m.CreatedAt != nil {
		m.CreatedAt = nil
		cleaned = true
	}
	if m.UpdatedAt != nil {
		m.UpdatedAt = nil
		cleaned = true
	}
	if m.ClosedAt != nil {
		m.ClosedAt = nil
		cleaned = true
	}
	if m.ClosedAt != nil {
		m.ClosedAt = nil
		cleaned = true
	}
	if m.DueOn != nil {
		m.DueOn = nil
		cleaned = true
	}
	if m.OpenIssues != nil {
		m.OpenIssues = nil
		cleaned = true
	}
	if m.ClosedIssues != nil {
		m.ClosedIssues = nil
		cleaned = true
	}
	return
}

func cleanReactions(r *github.Reactions) (cleaned bool) {
	cleanStr(&r.URL, &cleaned)
	for _, v := range []**int{
		&r.PlusOne,
		&r.MinusOne,
		&r.Laugh,
		&r.Confused,
		&r.Heart,
		&r.Hooray,
	} {
		if *v != nil && **v == 0 {
			*v = nil
		}
	}
	return
}

func cleanUser(u *github.User) (cleaned bool) {
	if u == nil {
		return false
	}
	for _, v := range []**string{
		&u.AvatarURL,
		&u.HTMLURL,
		&u.GravatarID,
		&u.URL,
		&u.EventsURL,
		&u.FollowingURL,
		&u.FollowersURL,
		&u.GistsURL,
		&u.OrganizationsURL,
		&u.ReceivedEventsURL,
		&u.ReposURL,
		&u.StarredURL,
		&u.SubscriptionsURL,
	} {
		cleanStr(v, &cleaned)
	}
	return
}

func cleanLabel(l *github.Label) (cleaned bool) {
	cleanStr(&l.URL, &cleaned)
	cleanStr(&l.Color, &cleaned)
	return
}

func cleanStr(pps **string, cleaned *bool) {
	if *pps != nil {
		*pps = nil
		*cleaned = true
	}
}

func writeDiskJSON(file string, modTime time.Time, v interface{}) (wrote bool, err error) {
	fi, err := os.Stat(file)
	if modTime.IsZero() {
		if err != nil {
			return false, err
		}
		modTime = fi.ModTime()
	} else if err == nil && fi.ModTime().Equal(modTime) {
		return false, nil
	}

	j, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return false, err
	}
	if j[len(j)-1] != '\n' {
		j = append(j, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return false, err
	}
	if err := ioutil.WriteFile(file, j, 0644); err != nil {
		return false, err
	}
	if err := os.Chtimes(file, modTime, modTime); err != nil {
		return false, err
	}

	log.Printf("Wrote %s", file)
	return true, nil
}
