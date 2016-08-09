// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package issuemirror provides access to mirrored Github issue data, cached
// on the local filesystem.
//
// For example, see https://github.com/bradfitz/go-issue-mirror.
package issuemirror

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/go-github/github"
)

// Root is the root directory of a repo's issue mirror on disk.
type Root string

// IssueJSONFile returns the path to the provided issue number's JSON
// file metadata.
func (r Root) IssueJSONFile(num int) string {
	return filepath.Join(string(r), "issues", fmt.Sprintf("%03d", num%1000), fmt.Sprint(num)+".json")
}

// IssueCommentsDir returns the path to the provided issue number's directory
// of comments.
func (r Root) IssueCommentsDir(num int) string {
	return filepath.Join(string(r), "issues", fmt.Sprintf("%03d", num%1000), fmt.Sprint(num)+".comments")
}

// IssueCommentFile returns the path to the provided comment's JSON
// file metadata.
func (r Root) IssueCommentFile(issueNum, commentID int) string {
	return filepath.Join(string(r), "issues", fmt.Sprintf("%03d", issueNum%1000), fmt.Sprint(issueNum)+".comments", "comment-"+strconv.Itoa(commentID)+".json")
}

// NumComments reports the number of comments on disk for the provided
// issue number.
func (r Root) NumComments(issueNum int) (int, error) {
	f, err := os.Open(r.IssueCommentsDir(issueNum))
	if os.IsNotExist(err) {
		if _, err := os.Stat(r.IssueJSONFile(issueNum)); err == nil {
			return 0, nil
		}
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return 0, err
	}
	return len(names), nil
}

// Issue returns the github issue from its cached JSON file on disk.
func (r Root) Issue(num int) (*github.Issue, error) {
	slurp, err := ioutil.ReadFile(r.IssueJSONFile(num))
	if err != nil {
		return nil, err
	}
	is := new(github.Issue)
	if err := json.Unmarshal(slurp, is); err != nil {
		return nil, err
	}
	return is, nil
}

// IssueComment returns the github issue comment from its cached JSON
// file on disk.
func (r Root) IssueComment(issueNum, commentID int) (*github.IssueComment, error) {
	slurp, err := ioutil.ReadFile(r.IssueCommentFile(issueNum, commentID))
	if err != nil {
		return nil, err
	}
	c := new(github.IssueComment)
	if err := json.Unmarshal(slurp, c); err != nil {
		return nil, err
	}
	return c, nil
}

// ForeachIssue iterates over each cached github issue, in numeric order.
// It stops at the first error. fn is not run concurrently.
func (r Root) ForeachIssue(fn func(*github.Issue) error) error {
	var nums []int
	issueDir := filepath.Join(string(r), "issues")
	if err := filepath.Walk(issueDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(issueDir, path)
		if err != nil {
			return err
		}
		if fi.IsDir() && len(rel) > 3 {
			return filepath.SkipDir
		}
		if fi.Mode().IsRegular() {
			base := filepath.Base(path)
			if strings.HasSuffix(base, ".json") {
				num, err := strconv.Atoi(strings.TrimSuffix(base, ".json"))
				if err != nil {
					return fmt.Errorf("unexpected non-numeric json file %s", path)
				}
				nums = append(nums, num)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Ints(nums)
	for _, num := range nums {
		is, err := r.Issue(num)
		if err != nil {
			return err
		}
		if err := fn(is); err != nil {
			return err
		}
	}
	return nil
}

// ForeachIssueComment iterates over each cached github issue comment
// for the provided issue number, in numeric order.
// It stops at the first error. fn is not run concurrently.
func (r Root) ForeachIssueComment(issueNum int, fn func(*github.IssueComment) error) error {
	var ids []int
	commentsDir := r.IssueCommentsDir(issueNum)
	if err := filepath.Walk(commentsDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if path == commentsDir {
				return nil
			}
			return fmt.Errorf("unexpected directory: %q", path)
		}
		if fi.Mode().IsRegular() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, "comment-") && strings.HasSuffix(base, ".json") {
				idStr := strings.TrimPrefix(strings.TrimSuffix(base, ".json"), "comment-")
				id, err := strconv.Atoi(idStr)
				if err != nil {
					return fmt.Errorf("unexpected non-numeric json file %s", path)
				}
				ids = append(ids, id)
			}
		}
		return nil
	}); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	sort.Ints(ids)
	for _, id := range ids {
		c, err := r.IssueComment(issueNum, id)
		if err != nil {
			return err
		}
		if err := fn(c); err != nil {
			return err
		}
	}
	return nil
}
