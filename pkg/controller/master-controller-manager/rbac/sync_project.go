/*
Copyright 2020 The Kubermatic Kubernetes Platform contributors.

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

package rbac

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	kubermaticv1 "k8c.io/kubermatic/v2/pkg/apis/kubermatic/v1"
	kuberneteshelper "k8c.io/kubermatic/v2/pkg/kubernetes"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	CleanupFinalizerName = "kubermatic.k8c.io/controller-manager-rbac-cleanup"
)

func (c *projectController) sync(ctx context.Context, key ctrlruntimeclient.ObjectKey) error {
	project := &kubermaticv1.Project{}
	if err := c.client.Get(ctx, key, project); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	if project.DeletionTimestamp != nil {
		if err := c.ensureProjectCleanup(ctx, project); err != nil {
			return fmt.Errorf("failed to cleanup project: %w", err)
		}
		return nil
	}

	// set the initial phase for new projects
	if project.Status.Phase == "" {
		if err := c.ensureProjectPhase(ctx, project, kubermaticv1.ProjectInactive); err != nil {
			return fmt.Errorf("failed to set initial project phase: %w", err)
		}
	}

	if err := c.ensureCleanupFinalizerExists(ctx, project); err != nil {
		return fmt.Errorf("failed to ensure that the cleanup finalizer exists on the project: %w", err)
	}
	if err := ensureClusterRBACRoleForNamedResource(ctx, c.log, c.client, project.Name, kubermaticv1.ProjectResourceName, kubermaticv1.ProjectKindName, project.GetObjectMeta()); err != nil {
		return fmt.Errorf("failed to ensure that the RBAC Role for the project exists: %w", err)
	}
	if err := ensureClusterRBACRoleBindingForNamedResource(ctx, c.log, c.client, project.Name, kubermaticv1.ProjectResourceName, kubermaticv1.ProjectKindName, project.GetObjectMeta()); err != nil {
		return fmt.Errorf("failed to ensure that the RBAC RoleBinding for the project exists: %w", err)
	}
	if err := c.ensureClusterRBACRoleForResources(ctx); err != nil {
		return fmt.Errorf("failed to ensure that the RBAC ClusterRoles for the project's resources exists: %w", err)
	}
	if err := c.ensureClusterRBACRoleBindingForResources(ctx, project.Name); err != nil {
		return fmt.Errorf("failed to ensure that the RBAC ClusterRoleBindings for the project's resources exists: %w", err)
	}
	if err := c.ensureRBACRoleForResources(ctx); err != nil {
		return fmt.Errorf("failed to ensure that the RBAC Roles for the project's resources exists: %w", err)
	}
	if err := c.ensureRBACRoleBindingForResources(ctx, project.Name); err != nil {
		return fmt.Errorf("failed to ensure that the RBAC RolesBindings for the project's resources exists: %w", err)
	}
	if err := c.ensureProjectPhase(ctx, project, kubermaticv1.ProjectActive); err != nil {
		return fmt.Errorf("failed to set project phase to active: %w", err)
	}

	return nil
}

func (c *projectController) ensureCleanupFinalizerExists(ctx context.Context, project *kubermaticv1.Project) error {
	return kuberneteshelper.TryAddFinalizer(ctx, c.client, project, CleanupFinalizerName)
}

func (c *projectController) ensureProjectPhase(ctx context.Context, project *kubermaticv1.Project, phase kubermaticv1.ProjectPhase) error {
	if project.Status.Phase != phase {
		oldProject := project.DeepCopy()
		project.Status.Phase = phase
		return c.client.Status().Patch(ctx, project, ctrlruntimeclient.MergeFrom(oldProject))
	}

	return nil
}

func (c *projectController) ensureClusterRBACRoleForResources(ctx context.Context) error {
	for _, projectResource := range c.projectResources {
		if len(projectResource.namespace) > 0 {
			continue
		}

		gvk := projectResource.object.GetObjectKind().GroupVersionKind()
		rmapping, err := c.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		for _, groupPrefix := range AllGroupsPrefixes {
			if projectResource.destination == destinationSeed {
				for _, seedClusterRESTClient := range c.seedClientMap {
					err := ensureClusterRBACRoleForResource(ctx, c.log, seedClusterRESTClient, groupPrefix, rmapping.Resource.Resource, gvk.Kind)
					if err != nil {
						return err
					}
				}
			} else {
				err := ensureClusterRBACRoleForResource(ctx, c.log, c.client, groupPrefix, rmapping.Resource.Resource, gvk.Kind)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *projectController) ensureClusterRBACRoleBindingForResources(ctx context.Context, projectName string) error {
	for _, projectResource := range c.projectResources {
		if len(projectResource.namespace) > 0 {
			continue
		}

		gvk := projectResource.object.GetObjectKind().GroupVersionKind()
		rmapping, err := c.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		for _, groupPrefix := range AllGroupsPrefixes {
			groupName := GenerateActualGroupNameFor(projectName, groupPrefix)

			if skip, err := shouldSkipClusterRBACRoleBindingFor(c.log, groupName, rmapping.Resource.Resource, kubermaticv1.SchemeGroupVersion.Group, projectName, gvk.Kind); skip {
				continue
			} else if err != nil {
				return err
			}

			if projectResource.destination == destinationSeed {
				for _, seedClusterRESTClient := range c.seedClientMap {
					err := ensureClusterRBACRoleBindingForResource(
						ctx,
						seedClusterRESTClient,
						groupName,
						rmapping.Resource.Resource)
					if err != nil {
						return err
					}
				}
			} else {
				err := ensureClusterRBACRoleBindingForResource(
					ctx,
					c.client,
					groupName,
					rmapping.Resource.Resource)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func ensureClusterRBACRoleForResource(ctx context.Context, log *zap.SugaredLogger, c ctrlruntimeclient.Client, groupName, resource, kind string) error {
	generatedClusterRole, err := generateClusterRBACRoleForResource(groupName, resource, kubermaticv1.SchemeGroupVersion.Group, kind)
	if err != nil {
		return err
	}
	if generatedClusterRole == nil {
		log.Debugw("skipping ClusterRole generation", "group", groupName, "resource", resource)
		return nil
	}

	var sharedExistingClusterRole rbacv1.ClusterRole
	key := types.NamespacedName{Name: generatedClusterRole.Name}
	if err := c.Get(ctx, key, &sharedExistingClusterRole); err != nil {
		if apierrors.IsNotFound(err) {
			return c.Create(ctx, generatedClusterRole)
		}

		return err
	}

	if equality.Semantic.DeepEqual(sharedExistingClusterRole.Rules, generatedClusterRole.Rules) {
		return nil
	}

	existingClusterRole := sharedExistingClusterRole.DeepCopy()
	existingClusterRole.Rules = generatedClusterRole.Rules
	return c.Update(ctx, existingClusterRole)
}

func ensureClusterRBACRoleBindingForResource(ctx context.Context, c ctrlruntimeclient.Client, groupName, resource string) error {
	generatedClusterRoleBinding := generateClusterRBACRoleBindingForResource(resource, groupName)

	var sharedExistingClusterRoleBinding rbacv1.ClusterRoleBinding
	key := types.NamespacedName{Name: generatedClusterRoleBinding.Name}
	if err := c.Get(ctx, key, &sharedExistingClusterRoleBinding); err != nil {
		if apierrors.IsNotFound(err) {
			return c.Create(ctx, generatedClusterRoleBinding)
		}

		return err
	}

	subjectsToAdd := []rbacv1.Subject{}

	for _, generatedRoleBindingSubject := range generatedClusterRoleBinding.Subjects {
		shouldAdd := true
		for _, existingRoleBindingSubject := range sharedExistingClusterRoleBinding.Subjects {
			if equality.Semantic.DeepEqual(existingRoleBindingSubject, generatedRoleBindingSubject) {
				shouldAdd = false
				break
			}
		}
		if shouldAdd {
			subjectsToAdd = append(subjectsToAdd, generatedRoleBindingSubject)
		}
	}

	if len(subjectsToAdd) == 0 {
		return nil
	}

	existingClusterRoleBinding := sharedExistingClusterRoleBinding.DeepCopy()
	existingClusterRoleBinding.Subjects = append(existingClusterRoleBinding.Subjects, subjectsToAdd...)
	return c.Update(ctx, existingClusterRoleBinding)
}

func (c *projectController) ensureRBACRoleForResources(ctx context.Context) error {
	for _, projectResource := range c.projectResources {
		if len(projectResource.namespace) == 0 {
			continue
		}

		gvk := projectResource.object.GetObjectKind().GroupVersionKind()
		rmapping, err := c.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		for _, groupPrefix := range AllGroupsPrefixes {
			if projectResource.destination == destinationSeed {
				for _, seedClusterRESTClient := range c.seedClientMap {
					err := ensureRBACRoleForResource(
						ctx,
						c.log,
						seedClusterRESTClient,
						groupPrefix,
						rmapping.Resource,
						gvk.Kind,
						projectResource.namespace)
					if err != nil {
						return err
					}
				}
			} else {
				err := ensureRBACRoleForResource(
					ctx,
					c.log,
					c.client,
					groupPrefix,
					rmapping.Resource,
					gvk.Kind,
					projectResource.namespace)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func ensureRBACRoleForResource(ctx context.Context, log *zap.SugaredLogger, c ctrlruntimeclient.Client, groupName string, gvr schema.GroupVersionResource, kind string, namespace string) error {
	generatedRole, err := generateRBACRoleForResource(groupName, gvr.Resource, gvr.Group, kind, namespace)
	if err != nil {
		return err
	}
	if generatedRole == nil {
		log.Debugw("skipping Role generation", "group", groupName, "resource", gvr.Resource, "namespace", namespace)
		return nil
	}
	var sharedExistingRole rbacv1.Role
	key := types.NamespacedName{Name: generatedRole.Name, Namespace: generatedRole.Namespace}
	if err := c.Get(ctx, key, &sharedExistingRole); err != nil {
		if apierrors.IsNotFound(err) {
			return c.Create(ctx, generatedRole)
		}
		return err
	}

	if equality.Semantic.DeepEqual(sharedExistingRole.Rules, generatedRole.Rules) {
		return nil
	}
	existingRole := sharedExistingRole.DeepCopy()
	existingRole.Rules = generatedRole.Rules
	return c.Update(ctx, existingRole)
}

func (c *projectController) ensureRBACRoleBindingForResources(ctx context.Context, projectName string) error {
	for _, projectResource := range c.projectResources {
		if len(projectResource.namespace) == 0 {
			continue
		}

		gvk := projectResource.object.GetObjectKind().GroupVersionKind()
		rmapping, err := c.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		for _, groupPrefix := range AllGroupsPrefixes {
			groupName := GenerateActualGroupNameFor(projectName, groupPrefix)

			if skip, err := shouldSkipRBACRoleBindingFor(c.log, groupName, rmapping.Resource.Resource, kubermaticv1.SchemeGroupVersion.Group, projectName, gvk.Kind, projectResource.namespace); skip {
				continue
			} else if err != nil {
				return err
			}

			if projectResource.destination == destinationSeed {
				for _, seedClusterRESTClient := range c.seedClientMap {
					err := ensureRBACRoleBindingForResource(
						ctx,
						seedClusterRESTClient,
						groupName,
						rmapping.Resource.Resource,
						projectResource.namespace)
					if err != nil {
						return err
					}
				}
			} else {
				err := ensureRBACRoleBindingForResource(
					ctx,
					c.client,
					groupName,
					rmapping.Resource.Resource,
					projectResource.namespace)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func ensureRBACRoleBindingForResource(ctx context.Context, c ctrlruntimeclient.Client, groupName, resource, namespace string) error {
	generatedRoleBinding := generateRBACRoleBindingForResource(resource, groupName, namespace)

	var sharedExistingRoleBinding rbacv1.RoleBinding
	key := types.NamespacedName{Name: generatedRoleBinding.Name, Namespace: generatedRoleBinding.Namespace}
	if err := c.Get(ctx, key, &sharedExistingRoleBinding); err != nil {
		if apierrors.IsNotFound(err) {
			return c.Create(ctx, generatedRoleBinding)
		}
		return err
	}

	subjectsToAdd := []rbacv1.Subject{}

	for _, generatedRoleBindingSubject := range generatedRoleBinding.Subjects {
		shouldAdd := true
		for _, existingRoleBindingSubject := range sharedExistingRoleBinding.Subjects {
			if equality.Semantic.DeepEqual(existingRoleBindingSubject, generatedRoleBindingSubject) {
				shouldAdd = false
				break
			}
		}
		if shouldAdd {
			subjectsToAdd = append(subjectsToAdd, generatedRoleBindingSubject)
		}
	}

	if len(subjectsToAdd) == 0 {
		return nil
	}

	existingRoleBinding := sharedExistingRoleBinding.DeepCopy()
	existingRoleBinding.Subjects = append(existingRoleBinding.Subjects, subjectsToAdd...)
	return c.Update(ctx, existingRoleBinding)
}

// ensureProjectCleanup ensures proper clean up of dependent resources upon deletion
//
// In particular:
// - removes no longer needed Subject from RBAC Binding for project's resources
// - removes cleanupFinalizer.
func (c *projectController) ensureProjectCleanup(ctx context.Context, project *kubermaticv1.Project) error {
	if err := c.ensureProjectPhase(ctx, project, kubermaticv1.ProjectTerminating); err != nil {
		return fmt.Errorf("failed to set project phase: %w", err)
	}

	// remove subjects from Cluster RBAC Bindings for project's resources
	for _, projectResource := range c.projectResources {
		if len(projectResource.namespace) > 0 {
			continue
		}

		gvk := projectResource.object.GetObjectKind().GroupVersionKind()
		rmapping, err := c.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		for _, groupPrefix := range AllGroupsPrefixes {
			groupName := GenerateActualGroupNameFor(project.Name, groupPrefix)
			if skip, err := shouldSkipClusterRBACRoleBindingFor(c.log, groupName, rmapping.Resource.Resource, kubermaticv1.SchemeGroupVersion.Group, project.Name, gvk.Kind); skip {
				continue
			} else if err != nil {
				return err
			}

			if projectResource.destination == destinationSeed {
				for _, seedClient := range c.seedClientMap {
					err := cleanUpClusterRBACRoleBindingFor(ctx, seedClient, groupName, rmapping.Resource.Resource)
					if err != nil {
						return err
					}
				}
			} else {
				err := cleanUpClusterRBACRoleBindingFor(ctx, c.client, groupName, rmapping.Resource.Resource)
				if err != nil {
					return err
				}
			}
		}
	}

	// remove subjects from RBAC Bindings for project's resources
	for _, projectResource := range c.projectResources {
		if len(projectResource.namespace) == 0 {
			continue
		}

		gvk := projectResource.object.GetObjectKind().GroupVersionKind()
		rmapping, err := c.restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		for _, groupPrefix := range AllGroupsPrefixes {
			groupName := GenerateActualGroupNameFor(project.Name, groupPrefix)
			if skip, err := shouldSkipRBACRoleBindingFor(c.log, groupName, rmapping.Resource.Resource, kubermaticv1.SchemeGroupVersion.Group, project.Name, gvk.Kind, projectResource.namespace); skip {
				continue
			} else if err != nil {
				return err
			}

			if projectResource.destination == destinationSeed {
				for _, seedClient := range c.seedClientMap {
					err := cleanUpRBACRoleBindingFor(ctx, seedClient, groupName, rmapping.Resource.Resource, projectResource.namespace)
					if err != nil {
						return err
					}
				}
			} else {
				err := cleanUpRBACRoleBindingFor(ctx, c.client, groupName, rmapping.Resource.Resource, projectResource.namespace)
				if err != nil {
					return err
				}
			}
		}
	}

	return kuberneteshelper.TryRemoveFinalizer(ctx, c.client, project, CleanupFinalizerName)
}

func cleanUpClusterRBACRoleBindingFor(ctx context.Context, c ctrlruntimeclient.Client, groupName, resource string) error {
	generatedClusterRoleBinding := generateClusterRBACRoleBindingForResource(resource, groupName)
	var sharedExistingClusterRoleBinding rbacv1.ClusterRoleBinding
	key := types.NamespacedName{Name: generatedClusterRoleBinding.Name}
	if err := c.Get(ctx, key, &sharedExistingClusterRoleBinding); err != nil {
		return err
	}

	updatedListOfSubjectes := []rbacv1.Subject{}
	for _, existingRoleBindingSubject := range sharedExistingClusterRoleBinding.Subjects {
		shouldRemove := false
		for _, generatedRoleBindingSubject := range generatedClusterRoleBinding.Subjects {
			if equality.Semantic.DeepEqual(existingRoleBindingSubject, generatedRoleBindingSubject) {
				shouldRemove = true
				break
			}
		}
		if !shouldRemove {
			updatedListOfSubjectes = append(updatedListOfSubjectes, existingRoleBindingSubject)
		}
	}

	existingClusterRoleBinding := sharedExistingClusterRoleBinding.DeepCopy()
	existingClusterRoleBinding.Subjects = updatedListOfSubjectes

	return c.Update(ctx, existingClusterRoleBinding)
}

func cleanUpRBACRoleBindingFor(ctx context.Context, c ctrlruntimeclient.Client, groupName, resource, namespace string) error {
	generatedRoleBinding := generateRBACRoleBindingForResource(resource, groupName, namespace)
	var sharedExistingRoleBinding rbacv1.RoleBinding
	key := types.NamespacedName{Name: generatedRoleBinding.Name, Namespace: namespace}
	if err := c.Get(ctx, key, &sharedExistingRoleBinding); err != nil {
		return err
	}

	updatedListOfSubjectes := []rbacv1.Subject{}
	for _, existingRoleBindingSubject := range sharedExistingRoleBinding.Subjects {
		shouldRemove := false
		for _, generatedRoleBindingSubject := range generatedRoleBinding.Subjects {
			if equality.Semantic.DeepEqual(existingRoleBindingSubject, generatedRoleBindingSubject) {
				shouldRemove = true
				break
			}
		}
		if !shouldRemove {
			updatedListOfSubjectes = append(updatedListOfSubjectes, existingRoleBindingSubject)
		}
	}

	existingRoleBinding := sharedExistingRoleBinding.DeepCopy()
	existingRoleBinding.Subjects = updatedListOfSubjectes
	return c.Update(ctx, existingRoleBinding)
}

// for some groups we actually don't create ClusterRole
// thus before doing something with ClusterRoleBinding check if the role was generated for the given resource and the group
//
// note: this method will add status to the log file
func shouldSkipClusterRBACRoleBindingFor(log *zap.SugaredLogger, groupName, policyResource, policyAPIGroups, projectName, kind string) (bool, error) {
	generatedClusterRole, err := generateClusterRBACRoleForResource(groupName, policyResource, policyAPIGroups, kind)
	if err != nil {
		return false, err
	}
	if generatedClusterRole == nil {
		log.Debugw("skipping operation on ClusterRoleBinding because corresponding ClusterRole was not (will not be) created", "group", groupName, "resource", policyResource, "project", projectName)
		return true, nil
	}
	return false, nil
}

// for some groups we actually don't create Role
// thus before doing something with RoleBinding check if the role was generated for the given resource and the group
//
// note: this method will add status to the log file
func shouldSkipRBACRoleBindingFor(log *zap.SugaredLogger, groupName, policyResource, policyAPIGroups, projectName, kind, namespace string) (bool, error) {
	generatedRole, err := generateRBACRoleForResource(groupName, policyResource, policyAPIGroups, kind, namespace)
	if err != nil {
		return false, err
	}
	if generatedRole == nil {
		log.Debugw("skipping operation on RoleBinding because corresponding Role was not (will not be) created", "group", groupName, "resource", policyResource, "project", projectName, "namespace", namespace)
		return true, nil
	}
	return false, nil
}
