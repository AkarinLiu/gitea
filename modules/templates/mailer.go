// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package templates

import (
	"context"
	"html/template"
	"io/fs"
	"os"
	"strings"
	texttmpl "text/template"

	"code.gitea.io/gitea/modules/base"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/watcher"
)

// mailSubjectTextFuncMap returns functions for injecting to text templates, it's only used for mail subject
func mailSubjectTextFuncMap() texttmpl.FuncMap {
	return texttmpl.FuncMap{
		"dict": dict,
		"Eval": Eval,

		"EllipsisString": base.EllipsisString,
		"AppName": func() string {
			return setting.AppName
		},
		"AppDomain": func() string { // documented in mail-templates.md
			return setting.Domain
		},
	}
}

func buildSubjectBodyTemplate(stpl *texttmpl.Template, btpl *template.Template, name string, content []byte) {
	// Split template into subject and body
	var subjectContent []byte
	bodyContent := content
	loc := mailSubjectSplit.FindIndex(content)
	if loc != nil {
		subjectContent = content[0:loc[0]]
		bodyContent = content[loc[1]:]
	}
	if _, err := stpl.New(name).
		Parse(string(subjectContent)); err != nil {
		log.Warn("Failed to parse template [%s/subject]: %v", name, err)
	}
	if _, err := btpl.New(name).
		Parse(string(bodyContent)); err != nil {
		log.Warn("Failed to parse template [%s/body]: %v", name, err)
	}
}

// Mailer provides the templates required for sending notification mails.
func Mailer(ctx context.Context) (*texttmpl.Template, *template.Template) {
	subjectTemplates := texttmpl.New("")
	bodyTemplates := template.New("")

	subjectTemplates.Funcs(mailSubjectTextFuncMap())
	for _, funcs := range NewFuncMap() {
		bodyTemplates.Funcs(funcs)
	}

	refreshTemplates := func() {
		for _, assetPath := range BuiltinAssetNames() {
			if !strings.HasPrefix(assetPath, "mail/") {
				continue
			}

			if !strings.HasSuffix(assetPath, ".tmpl") {
				continue
			}

			content, err := BuiltinAsset(assetPath)
			if err != nil {
				log.Warn("Failed to read embedded %s template. %v", assetPath, err)
				continue
			}

			assetName := strings.TrimPrefix(strings.TrimSuffix(assetPath, ".tmpl"), "mail/")

			log.Trace("Adding built-in mailer template for %s", assetName)
			buildSubjectBodyTemplate(subjectTemplates,
				bodyTemplates,
				assetName,
				content)
		}

		if err := walkMailerTemplates(func(path, name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				log.Warn("Failed to read custom %s template. %v", path, err)
				return nil
			}

			assetName := strings.TrimSuffix(name, ".tmpl")
			log.Trace("Adding mailer template for %s from %q", assetName, path)
			buildSubjectBodyTemplate(subjectTemplates,
				bodyTemplates,
				assetName,
				content)
			return nil
		}); err != nil && !os.IsNotExist(err) {
			log.Warn("Error whilst walking mailer templates directories. %v", err)
		}
	}

	refreshTemplates()

	if !setting.IsProd {
		// Now subjectTemplates and bodyTemplates are both synchronized
		// thus it is safe to call refresh from a different goroutine
		watcher.CreateWatcher(ctx, "Mailer Templates", &watcher.CreateWatcherOpts{
			PathsCallback:   walkMailerTemplates,
			BetweenCallback: refreshTemplates,
		})
	}

	return subjectTemplates, bodyTemplates
}
