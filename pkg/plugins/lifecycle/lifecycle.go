/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lifecycle

import (
	"regexp"

	"github.com/jenkins-x/go-scm/scm"
	"github.com/jenkins-x/lighthouse/pkg/scmprovider"
	"github.com/sirupsen/logrus"

	"github.com/jenkins-x/lighthouse/pkg/labels"
	"github.com/jenkins-x/lighthouse/pkg/pluginhelp"
	"github.com/jenkins-x/lighthouse/pkg/plugins"
)

var (
	lifecycleLabels = []string{labels.LifecycleActive, labels.LifecycleFrozen, labels.LifecycleStale, labels.LifecycleRotten}
	lifecycleRe     = regexp.MustCompile(`(?mi)^/(?:lh-)?(remove-)?lifecycle (active|frozen|stale|rotten)\s*$`)
)

const pluginName = "lifecycle"

var (
	plugin = plugins.Plugin{
		Description:  "Close, reopen, flag and/or unflag an issue or PR as frozen/stale/rotten",
		HelpProvider: help,
		Commands: []plugins.Command{{
			Filter:                func(e scmprovider.GenericCommentEvent) bool { return e.Action == scm.ActionCreate },
			GenericCommentHandler: lifecycleHandleGenericComment,
			Help: []pluginhelp.Command{{
				Usage:       "/close",
				Description: "Closes an issue or PR.",
				Featured:    false,
				WhoCanUse:   "Authors and collaborators on the repository can trigger this command.",
				Examples:    []string{"/close", "/lh-close"},
			}, {
				Usage:       "/reopen",
				Description: "Reopens an issue or PR",
				Featured:    false,
				WhoCanUse:   "Authors and collaborators on the repository can trigger this command.",
				Examples:    []string{"/reopen", "/lh-reopen"},
			}, {
				Usage:       "/[remove-]lifecycle <frozen|stale|rotten>",
				Description: "Flags an issue or PR as frozen/stale/rotten",
				Featured:    false,
				WhoCanUse:   "Anyone can trigger this command.",
				Examples:    []string{"/lifecycle frozen", "/remove-lifecycle stale", "/lh-lifecyle rotten"},
			}},
		}},
	}
)

func init() {
	plugins.RegisterPlugin(pluginName, plugin)
}

func help(config *plugins.Configuration, enabledRepos []string) (*pluginhelp.PluginHelp, error) {
	return &pluginhelp.PluginHelp{}, nil
}

type lifecycleClient interface {
	AddLabel(owner, repo string, number int, label string, pr bool) error
	RemoveLabel(owner, repo string, number int, label string, pr bool) error
	GetIssueLabels(org, repo string, number int, pr bool) ([]*scm.Label, error)
}

func lifecycleHandleGenericComment(_ []string, pc plugins.Agent, e scmprovider.GenericCommentEvent) error {
	gc := pc.SCMProviderClient
	log := pc.Logger
	if err := handleReopen(gc, log, &e); err != nil {
		return err
	}
	if err := handleClose(gc, log, &e); err != nil {
		return err
	}
	return handle(gc, log, &e)
}

func handle(gc lifecycleClient, log *logrus.Entry, e *scmprovider.GenericCommentEvent) error {
	for _, mat := range lifecycleRe.FindAllStringSubmatch(e.Body, -1) {
		if err := handleOne(gc, log, e, mat); err != nil {
			return err
		}
	}
	return nil
}

func handleOne(gc lifecycleClient, log *logrus.Entry, e *scmprovider.GenericCommentEvent, mat []string) error {
	org := e.Repo.Namespace
	repo := e.Repo.Name
	number := e.Number

	remove := mat[1] != ""
	cmd := mat[2]
	lbl := "lifecycle/" + cmd

	// Let's start simple and allow anyone to add/remove frozen, stale, rotten labels.
	// Adjust if we find evidence of the community abusing these labels.
	labels, err := gc.GetIssueLabels(org, repo, number, e.IsPR)
	if err != nil {
		log.WithError(err).Errorf("Failed to get labels.")
	}

	// If the label exists and we asked for it to be removed, remove it.
	if scmprovider.HasLabel(lbl, labels) && remove {
		return gc.RemoveLabel(org, repo, number, lbl, e.IsPR)
	}

	// If the label does not exist and we asked for it to be added,
	// remove other existing lifecycle labels and add it.
	if !scmprovider.HasLabel(lbl, labels) && !remove {
		for _, label := range lifecycleLabels {
			if label != lbl && scmprovider.HasLabel(label, labels) {
				if err := gc.RemoveLabel(org, repo, number, label, e.IsPR); err != nil {
					log.WithError(err).Errorf("GitHub failed to remove the following label: %s", label)
				}
			}
		}

		if err := gc.AddLabel(org, repo, number, lbl, e.IsPR); err != nil {
			log.WithError(err).Errorf("GitHub failed to add the following label: %s", lbl)
		}
	}

	return nil
}
