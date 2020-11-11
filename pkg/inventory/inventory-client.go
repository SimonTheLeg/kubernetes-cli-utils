// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"fmt"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/klog"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/validation"
	"sigs.k8s.io/cli-utils/pkg/apply/info"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/ordering"
)

// InventoryClient expresses an interface for interacting with
// objects which store references to objects (inventory objects).
type InventoryClient interface {
	// GetCluster returns the set of previously applied objects as ObjMetadata,
	// or an error if one occurred. This set of previously applied object references
	// is stored in the inventory objects living in the cluster.
	GetClusterObjs(inv *unstructured.Unstructured) ([]object.ObjMetadata, error)
	// Merge applies the union of the passed objects with the currently
	// stored objects in the inventory object. Returns the slice of
	// objects which are a set diff (objects to be pruned). Otherwise,
	// returns an error if one happened.
	Merge(inv *unstructured.Unstructured, objs []object.ObjMetadata) ([]object.ObjMetadata, error)
	// Replace replaces the set of objects stored in the inventory
	// object with the passed set of objects, or an error if one occurs.
	Replace(inv *unstructured.Unstructured, objs []object.ObjMetadata) error
	// DeleteInventoryObj deletes the passed inventory object from the APIServer.
	DeleteInventoryObj(inv *unstructured.Unstructured) error
	// SetDryRunStrategy sets the dry run strategy on whether this we actually mutate.
	SetDryRunStrategy(drs common.DryRunStrategy)
	ApplyInventoryNamespace(invNamespace *unstructured.Unstructured) error
}

// ClusterInventoryClient is a concrete implementation of the
// InventoryClient interface.
type ClusterInventoryClient struct {
	builderFunc          func() *resource.Builder
	mapper               meta.RESTMapper
	validator            validation.Schema
	clientFunc           func(*meta.RESTMapping) (resource.RESTClient, error)
	dryRunStrategy       common.DryRunStrategy
	InventoryFactoryFunc InventoryFactoryFunc
	infoHelper           info.InfoHelper
}

var _ InventoryClient = &ClusterInventoryClient{}

// NewInventoryClient returns a concrete implementation of the
// InventoryClient interface or an error.
func NewInventoryClient(factory cmdutil.Factory, invFunc InventoryFactoryFunc) (*ClusterInventoryClient, error) {
	var err error
	mapper, err := factory.ToRESTMapper()
	if err != nil {
		return nil, err
	}
	validator, err := factory.Validator(false)
	if err != nil {
		return nil, err
	}
	builderFunc := factory.NewBuilder
	clusterInventoryClient := ClusterInventoryClient{
		builderFunc:          builderFunc,
		mapper:               mapper,
		validator:            validator,
		clientFunc:           factory.UnstructuredClientForMapping,
		dryRunStrategy:       common.DryRunNone,
		InventoryFactoryFunc: invFunc,
		infoHelper:           info.NewInfoHelper(factory),
	}
	return &clusterInventoryClient, nil
}

// Merge stores the union of the passed objects with the objects currently
// stored in the cluster inventory object. Retrieves and caches the cluster
// inventory object. Returns the set differrence of the cluster inventory
// objects and the currently applied objects. This is the set of objects
// to prune. Creates the initial cluster inventory object storing the passed
// objects if an inventory object does not exist. Returns an error if one
// occurred.
func (cic *ClusterInventoryClient) Merge(localInv *unstructured.Unstructured, objs []object.ObjMetadata) ([]object.ObjMetadata, error) {
	pruneIds := []object.ObjMetadata{}
	clusterInv, err := cic.getClusterInventoryInfo(localInv)
	if err != nil {
		return pruneIds, err
	}
	if clusterInv == nil {
		// Wrap inventory object and store the inventory in it.
		inv := cic.InventoryFactoryFunc(localInv)
		if err := inv.Store(objs); err != nil {
			return nil, err
		}
		invInfo, err := inv.GetObject()
		if err != nil {
			return nil, err
		}
		klog.V(4).Infof("creating initial inventory object with %d objects", len(objs))
		if err := cic.createInventoryObj(invInfo); err != nil {
			return nil, err
		}
	} else {
		// Update existing cluster inventory with merged union of objects
		clusterObjs, err := cic.GetClusterObjs(localInv)
		if err != nil {
			return pruneIds, err
		}
		if object.SetEquals(objs, clusterObjs) {
			klog.V(4).Infof("applied objects same as cluster inventory: do nothing")
			return pruneIds, nil
		}
		pruneIds = object.SetDiff(clusterObjs, objs)
		unionObjs := object.Union(clusterObjs, objs)
		klog.V(4).Infof("num objects to prune: %d", len(pruneIds))
		klog.V(4).Infof("num merged objects to store in inventory: %d", len(unionObjs))
		wrappedInv := cic.InventoryFactoryFunc(clusterInv)
		if err = wrappedInv.Store(unionObjs); err != nil {
			return pruneIds, err
		}
		if !cic.dryRunStrategy.ClientOrServerDryRun() {
			clusterInv, err = wrappedInv.GetObject()
			if err != nil {
				return pruneIds, err
			}
			klog.V(4).Infof("update cluster inventory: %s/%s", clusterInv.GetNamespace(), clusterInv.GetName())
			if err := cic.applyInventoryObj(clusterInv); err != nil {
				return pruneIds, err
			}
		}
	}

	return pruneIds, nil
}

// Replace stores the passed objects in the cluster inventory object, or
// an error if one occurred.
func (cic *ClusterInventoryClient) Replace(localInv *unstructured.Unstructured, objs []object.ObjMetadata) error {
	clusterObjs, err := cic.GetClusterObjs(localInv)
	if err != nil {
		return err
	}
	if object.SetEquals(objs, clusterObjs) {
		klog.V(4).Infof("applied objects same as cluster inventory: do nothing")
		return nil
	}
	clusterInv, err := cic.getClusterInventoryInfo(localInv)
	if err != nil {
		return err
	}
	wrappedInv := cic.InventoryFactoryFunc(clusterInv)
	if err = wrappedInv.Store(objs); err != nil {
		return err
	}
	if !cic.dryRunStrategy.ClientOrServerDryRun() {
		clusterInv, err = wrappedInv.GetObject()
		if err != nil {
			return err
		}
		klog.V(4).Infof("replace cluster inventory: %s/%s", clusterInv.GetNamespace(), clusterInv.GetName())
		klog.V(4).Infof("replace cluster inventory %d objects", len(objs))
		if err := cic.applyInventoryObj(clusterInv); err != nil {
			return err
		}
	}
	return nil
}

// GetClusterObjs returns the objects stored in the cluster inventory object, or
// an error if one occurred.
func (cic *ClusterInventoryClient) GetClusterObjs(localInv *unstructured.Unstructured) ([]object.ObjMetadata, error) {
	var objs []object.ObjMetadata
	clusterInv, err := cic.getClusterInventoryInfo(localInv)
	if err != nil {
		return objs, err
	}
	// First time; no inventory obj yet.
	if clusterInv == nil {
		return []object.ObjMetadata{}, nil
	}
	wrapped := cic.InventoryFactoryFunc(clusterInv)
	return wrapped.Load()
}

// getClusterInventoryObj returns a pointer to the cluster inventory object, or
// an error if one occurred. Returns the cached cluster inventory object if it
// has been previously retrieved. Uses the ResourceBuilder to retrieve the
// inventory object in the cluster, using the namespace, group resource, and
// inventory label. Merges multiple inventory objects into one if it retrieves
// more than one (this should be very rare).
//
// TODO(seans3): Remove the special case code to merge multiple cluster inventory
// objects once we've determined that this case is no longer possible.
func (cic *ClusterInventoryClient) getClusterInventoryInfo(localInv *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if localInv == nil {
		return nil, fmt.Errorf("retrieving cluster inventory object with nil local inventory")
	}
	localObj := object.UnstructuredToObjMeta(localInv)
	mapping, err := cic.mapper.RESTMapping(localObj.GroupKind)
	if err != nil {
		return nil, err
	}
	groupResource := mapping.Resource.GroupResource().String()
	namespace := localObj.Namespace
	label, err := retrieveInventoryLabel(localInv)
	if err != nil {
		return nil, err
	}
	labelSelector := fmt.Sprintf("%s=%s", common.InventoryLabel, label)
	klog.V(4).Infof("prune inventory object fetch: %s/%s/%s", groupResource, namespace, labelSelector)
	builder := cic.builderFunc()
	retrievedInventoryInfos, err := builder.
		Unstructured().
		// TODO: Check if this validator is necessary.
		Schema(cic.validator).
		ContinueOnError().
		NamespaceParam(namespace).DefaultNamespace().
		ResourceTypes(groupResource).
		LabelSelectorParam(labelSelector).
		Flatten().
		Do().
		Infos()
	if err != nil {
		return nil, err
	}
	var clusterInv *unstructured.Unstructured
	if len(retrievedInventoryInfos) == 1 {
		clusterInv = object.InfoToUnstructured(retrievedInventoryInfos[0])
	} else if len(retrievedInventoryInfos) > 1 {
		clusterInv, err = cic.mergeClusterInventory(object.InfosToUnstructureds(retrievedInventoryInfos))
		if err != nil {
			return nil, err
		}
	}
	return clusterInv, nil
}

// mergeClusterInventory merges the inventory of multiple inventory objects
// into one inventory object, and applies it. Deletes the remaining unnecessary
// inventory objects. There should be only one inventory object stored in the
// cluster after this function. This special case should be very rare.
//
// TODO(seans3): Remove this code once we're certain no customers have multiple
// inventory objects in their clusters.
func (cic *ClusterInventoryClient) mergeClusterInventory(invObjs []*unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if len(invObjs) == 0 {
		return nil, nil
	}
	klog.V(4).Infof("merging %d inventory objects", len(invObjs))
	// Make the selection of the retained inventory info deterministic,
	// choosing the first inventory object as the one to retain.
	sort.Sort(ordering.SortableUnstructureds(invObjs))
	retained := invObjs[0]
	wrapRetained := cic.InventoryFactoryFunc(retained)
	retainedObjs, err := wrapRetained.Load()
	if err != nil {
		return nil, err
	}
	// Merge all the objects in the other inventory objects into
	// the retained objects.
	for i := 1; i < len(invObjs); i++ {
		merge := invObjs[i]
		wrapMerge := cic.InventoryFactoryFunc(merge)
		mergeObjs, err := wrapMerge.Load()
		if err != nil {
			return nil, err
		}
		retainedObjs = object.Union(retainedObjs, mergeObjs)
	}
	if err := wrapRetained.Store(retainedObjs); err != nil {
		return nil, err
	}
	retainInfo, err := wrapRetained.GetObject()
	if err != nil {
		return nil, err
	}
	// Store the merged inventory into the one retained inventory
	// object.
	//
	// IMPORTANT: This must happen BEFORE deleting the other
	// inventory objects, in order to ensure we always have
	// access to the union of the inventory.
	if err := cic.applyInventoryObj(retainInfo); err != nil {
		return nil, err
	}
	// Finally, delete the other inventory objects.
	for i := 1; i < len(invObjs); i++ {
		merge := invObjs[i]
		if err := cic.DeleteInventoryObj(merge); err != nil {
			return nil, err
		}
	}
	return retainInfo, nil
}

// applyInventoryObj applies the passed inventory object to the APIServer.
func (cic *ClusterInventoryClient) applyInventoryObj(obj *unstructured.Unstructured) error {
	if cic.dryRunStrategy.ClientOrServerDryRun() {
		klog.V(4).Infof("dry-run apply inventory object: not applied")
		return nil
	}
	if obj == nil {
		return fmt.Errorf("attempting apply a nil inventory object")
	}
	invInfo, err := cic.toInfo(obj)
	if err != nil {
		return err
	}
	helper := resource.NewHelper(invInfo.Client, invInfo.Mapping)
	klog.V(4).Infof("replacing inventory object: %s/%s", invInfo.Namespace, invInfo.Name)
	var overwrite = true
	replacedObj, err := helper.Replace(invInfo.Namespace, invInfo.Name, overwrite, invInfo.Object)
	if err != nil {
		return err
	}
	var ignoreError = true
	return invInfo.Refresh(replacedObj, ignoreError)
}

// createInventoryObj creates the passed inventory object on the APIServer.
func (cic *ClusterInventoryClient) createInventoryObj(obj *unstructured.Unstructured) error {
	if cic.dryRunStrategy.ClientOrServerDryRun() {
		klog.V(4).Infof("dry-run create inventory object: not created")
		return nil
	}
	if obj == nil {
		return fmt.Errorf("attempting create a nil inventory object")
	}
	// Default inventory name gets random suffix. Fixes problem where legacy
	// inventory templates within same namespace will collide on name.
	err := fixLegacyInventoryName(obj)
	if err != nil {
		return err
	}
	invInfo, err := cic.toInfo(obj)
	if err != nil {
		return err
	}
	helper, err := cic.helperFromInfo(invInfo)
	if err != nil {
		return err
	}
	klog.V(4).Infof("creating inventory object: %s/%s", invInfo.Namespace, invInfo.Name)
	var clearResourceVersion = false
	createdObj, err := helper.Create(invInfo.Namespace, clearResourceVersion, invInfo.Object)
	if err != nil {
		return err
	}
	var ignoreError = true
	return invInfo.Refresh(createdObj, ignoreError)
}

// DeleteInventoryObj deletes the passed inventory object from the APIServer, or
// an error if one occurs.
func (cic *ClusterInventoryClient) DeleteInventoryObj(obj *unstructured.Unstructured) error {
	if cic.dryRunStrategy.ClientOrServerDryRun() {
		klog.V(4).Infof("dry-run delete inventory object: not deleted")
		return nil
	}
	if obj == nil {
		return fmt.Errorf("attempting delete a nil inventory object")
	}
	invInfo, err := cic.toInfo(obj)
	if err != nil {
		return err
	}
	helper, err := cic.helperFromInfo(invInfo)
	if err != nil {
		return err
	}
	klog.V(4).Infof("deleting inventory object: %s/%s", invInfo.Namespace, invInfo.Name)
	_, err = helper.Delete(invInfo.Namespace, invInfo.Name)
	return err
}

// SetDryRun sets whether the inventory client will mutate the inventory
// object in the cluster.
func (cic *ClusterInventoryClient) SetDryRunStrategy(drs common.DryRunStrategy) {
	cic.dryRunStrategy = drs
}

// ApplyInventoryNamespace creates the passed namespace if it does not already
// exist, or returns an error if one happened. NOTE: No error if already exists.
func (cic *ClusterInventoryClient) ApplyInventoryNamespace(obj *unstructured.Unstructured) error {
	if cic.dryRunStrategy.ClientOrServerDryRun() {
		klog.V(4).Infof("dry-run apply inventory namespace (%s): not applied", obj.GetName())
		return nil
	}
	invInfo, err := cic.toInfo(obj)
	if err != nil {
		return err
	}
	helper, err := cic.helperFromInfo(invInfo)
	if err != nil {
		return err
	}
	klog.V(4).Infof("applying inventory namespace: %s", invInfo.Name)
	if err := util.CreateApplyAnnotation(invInfo.Object, unstructured.UnstructuredJSONScheme); err != nil {
		return err
	}
	var clearResourceVersion = false
	createdObj, err := helper.Create(invInfo.Namespace, clearResourceVersion, invInfo.Object)
	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		return nil
	}
	var ignoreError = true
	return invInfo.Refresh(createdObj, ignoreError)
}

func (cic *ClusterInventoryClient) toInfo(obj *unstructured.Unstructured) (*resource.Info, error) {
	invInfos, err := cic.infoHelper.BuildInfos([]*unstructured.Unstructured{obj})
	if err != nil {
		return nil, err
	}
	return invInfos[0], nil
}

// helperFromInfo returns the resource.Helper to talk to the APIServer based
// on the information from the passed "info", or an error if one occurred.
func (cic *ClusterInventoryClient) helperFromInfo(info *resource.Info) (*resource.Helper, error) {
	obj, err := object.InfoToObjMeta(info)
	if err != nil {
		return nil, err
	}
	mapping, err := cic.mapper.RESTMapping(obj.GroupKind)
	if err != nil {
		return nil, err
	}
	client, err := cic.clientFunc(mapping)
	if err != nil {
		return nil, err
	}
	return resource.NewHelper(client, mapping), nil
}
