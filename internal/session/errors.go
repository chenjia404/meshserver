package session

import "errors"

// ErrAdminRoleRequired is returned when LIST_SPACE_MEMBERS-style operations need owner or admin.
var ErrAdminRoleRequired = errors.New("admin role required")

// ErrOwnerRoleRequired is returned when only space owner may perform the action.
var ErrOwnerRoleRequired = errors.New("owner role required")

// ErrCreateSpacePermission is returned when site-wide admin is required to create a space.
var ErrCreateSpacePermission = errors.New("create space permission required")

// ErrAutoDeleteGroupOnly is returned when auto-delete is applied to a non-group channel.
var ErrAutoDeleteGroupOnly = errors.New("auto delete is only supported for group channels")
