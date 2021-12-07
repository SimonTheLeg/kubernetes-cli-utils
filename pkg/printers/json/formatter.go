// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/print/list"
)

func NewFormatter(ioStreams genericclioptions.IOStreams,
	previewStrategy common.DryRunStrategy) list.Formatter {
	return &formatter{
		ioStreams:       ioStreams,
		previewStrategy: previewStrategy,
	}
}

type formatter struct {
	previewStrategy common.DryRunStrategy
	ioStreams       genericclioptions.IOStreams
}

func (jf *formatter) FormatApplyEvent(ae event.ApplyEvent) error {
	eventInfo := jf.baseResourceEvent(ae.Identifier)
	if ae.Error != nil {
		eventInfo["error"] = ae.Error.Error()
		return jf.printEvent("apply", "resourceFailed", eventInfo)
	}
	eventInfo["operation"] = ae.Operation.String()
	return jf.printEvent("apply", "resourceApplied", eventInfo)
}

func (jf *formatter) FormatStatusEvent(se event.StatusEvent) error {
	return jf.printResourceStatus(se)
}

func (jf *formatter) printResourceStatus(se event.StatusEvent) error {
	eventInfo := jf.baseResourceEvent(se.Identifier)
	eventInfo["status"] = se.PollResourceInfo.Status.String()
	eventInfo["message"] = se.PollResourceInfo.Message
	return jf.printEvent("status", "resourceStatus", eventInfo)
}

func (jf *formatter) FormatPruneEvent(pe event.PruneEvent) error {
	eventInfo := jf.baseResourceEvent(pe.Identifier)
	if pe.Error != nil {
		eventInfo["error"] = pe.Error.Error()
		return jf.printEvent("prune", "resourceFailed", eventInfo)
	}
	eventInfo["operation"] = pe.Operation.String()
	return jf.printEvent("prune", "resourcePruned", eventInfo)
}

func (jf *formatter) FormatDeleteEvent(de event.DeleteEvent) error {
	eventInfo := jf.baseResourceEvent(de.Identifier)
	if de.Error != nil {
		eventInfo["error"] = de.Error.Error()
		return jf.printEvent("delete", "resourceFailed", eventInfo)
	}
	eventInfo["operation"] = de.Operation.String()
	return jf.printEvent("delete", "resourceDeleted", eventInfo)
}

func (jf *formatter) FormatWaitEvent(we event.WaitEvent) error {
	eventInfo := jf.baseResourceEvent(we.Identifier)
	eventInfo["operation"] = we.Operation.String()
	return jf.printEvent("wait", "resourceReconciled", eventInfo)
}

func (jf *formatter) FormatErrorEvent(ee event.ErrorEvent) error {
	return jf.printEvent("error", "error", map[string]interface{}{
		"error": ee.Err.Error(),
	})
}

func (jf *formatter) FormatActionGroupEvent(
	age event.ActionGroupEvent,
	ags []event.ActionGroup,
	as *list.ApplyStats,
	ps *list.PruneStats,
	ds *list.DeleteStats,
	ws *list.WaitStats,
	c list.Collector,
) error {
	if age.Action == event.ApplyAction && age.Type == event.Finished {
		if err := jf.printEvent("apply", "completed", map[string]interface{}{
			"count":           as.Sum(),
			"createdCount":    as.Created,
			"unchangedCount":  as.Unchanged,
			"configuredCount": as.Configured,
			"serverSideCount": as.ServersideApplied,
			"failedCount":     as.Failed,
		}); err != nil {
			return err
		}
	}

	if age.Action == event.PruneAction && age.Type == event.Finished {
		return jf.printEvent("prune", "completed", map[string]interface{}{
			"pruned":  ps.Pruned,
			"skipped": ps.Skipped,
		})
	}

	if age.Action == event.DeleteAction && age.Type == event.Finished {
		return jf.printEvent("delete", "completed", map[string]interface{}{
			"deleted": ds.Deleted,
			"skipped": ds.Skipped,
		})
	}

	if age.Action == event.WaitAction && age.Type == event.Started {
		ag, found := list.ActionGroupByName(age.GroupName, ags)
		if !found {
			panic(fmt.Errorf("unknown action group name %q", age.GroupName))
		}
		for id, se := range c.LatestStatus() {
			// Only print information about objects that we actually care about
			// for this wait task.
			if found := ag.Identifiers.Contains(id); found {
				if err := jf.printResourceStatus(se); err != nil {
					return err
				}
			}
		}
	}

	if age.Action == event.WaitAction && age.Type == event.Finished {
		return jf.printEvent("wait", "completed", map[string]interface{}{
			"reconciled": ws.Reconciled,
			"skipped":    ws.Skipped,
			"timeout":    ws.Timeout,
			"failed":     ws.Failed,
		})
	}

	return nil
}

func (jf *formatter) baseResourceEvent(identifier object.ObjMetadata) map[string]interface{} {
	return map[string]interface{}{
		"group":     identifier.GroupKind.Group,
		"kind":      identifier.GroupKind.Kind,
		"namespace": identifier.Namespace,
		"name":      identifier.Name,
	}
}

func (jf *formatter) printEvent(t, eventType string, content map[string]interface{}) error {
	m := make(map[string]interface{})
	m["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	m["type"] = t
	m["eventType"] = eventType
	for key, val := range content {
		m[key] = val
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(jf.ioStreams.Out, string(b)+"\n")
	return err
}
