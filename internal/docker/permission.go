package docker

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// permissionAdvice explains a refusal on the socket in terms of the group that
// owns it. hublot enforces nothing itself: the socket's mode is the access
// control, exactly as it is for the docker CLI. All this does is say which of
// the three situations the user is in, because the message that matters is not
// the same in each.
func permissionAdvice(socket string) string {
	group, err := socketGroup(socket)
	if err != nil || group == nil {
		return fmt.Sprintf("permission denied on %s: you may need to be in the group that owns it, or to run hublot as root", socket)
	}

	inProcess := hasGroup(processGroups(), group.gid)
	inDatabase := hasGroup(declaredGroups(), group.gid)

	return groupAdvice(socket, group.name, currentUserName(), inProcess, inDatabase)
}

// groupAdvice is the wording, kept free of any lookups so every branch can be
// tested without a socket, a group or a particular machine.
func groupAdvice(socket, group, username string, inProcess, inDatabase bool) string {
	switch {
	case inDatabase && !inProcess:
		// The one that wastes the most time: the membership exists, but this
		// session started before it and carries the old group list.
		return fmt.Sprintf(
			"permission denied on %s: you are in the '%s' group, but this session started before that. "+
				"Log out and back in, or start a shell with: newgrp %s",
			socket, group, group)

	case !inDatabase:
		return fmt.Sprintf(
			"permission denied on %s: add yourself to the '%s' group, then log out and back in: "+
				"sudo usermod -aG %s %s",
			socket, group, group, username)

	default:
		// Already a member, and the process carries it: something else is
		// refusing, so do not send the user chasing the group.
		return fmt.Sprintf(
			"permission denied on %s although you are in the '%s' group: check the socket's permissions, "+
				"or whether a security policy is blocking it",
			socket, group)
	}
}

type socketOwner struct {
	gid  uint32
	name string
}

// socketGroup reads which group owns the socket, and its name.
func socketGroup(socket string) (*socketOwner, error) {
	info, err := os.Stat(socket)
	if err != nil {
		return nil, err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("no ownership information for %s", socket)
	}

	owner := &socketOwner{gid: stat.Gid, name: strconv.FormatUint(uint64(stat.Gid), 10)}
	if group, err := user.LookupGroupId(owner.name); err == nil {
		owner.name = group.Name
	}
	return owner, nil
}

// processGroups is what this process actually carries, fixed at login.
func processGroups() []int {
	groups, err := syscall.Getgroups()
	if err != nil {
		return nil
	}
	return groups
}

// declaredGroups is what the group database says today, which is not the same
// thing the moment someone runs usermod.
func declaredGroups() []int {
	me, err := user.Current()
	if err != nil {
		return nil
	}
	ids, err := me.GroupIds()
	if err != nil {
		return nil
	}

	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if n, err := strconv.Atoi(id); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func currentUserName() string {
	if me, err := user.Current(); err == nil && me.Username != "" {
		return me.Username
	}
	return "$USER"
}

func hasGroup(groups []int, gid uint32) bool {
	for _, g := range groups {
		if uint32(g) == gid {
			return true
		}
	}
	return false
}
