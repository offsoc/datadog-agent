// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build linux

// Package probes holds probes related files
package probes

import (
	manager "github.com/DataDog/ebpf-manager"

	"github.com/DataDog/datadog-agent/pkg/security/secl/compiler/eval"
	"github.com/DataDog/datadog-agent/pkg/security/secl/model"
	"github.com/DataDog/datadog-agent/pkg/security/utils"
)

// NetworkNFNatSelectors is the list of probes that should be activated if the `nf_nat` module is loaded
func NetworkNFNatSelectors() []manager.ProbesSelector {
	return []manager.ProbesSelector{
		&manager.OneOf{Selectors: []manager.ProbesSelector{
			hookFunc("hook_nf_nat_manip_pkt"),
			hookFunc("hook_nf_nat_packet"),
			hookFunc("hook_nf_ct_delete"),
		}},
	}
}

// NetworkVethSelectors is the list of probes that should be activated if the `veth` module is loaded
func NetworkVethSelectors() []manager.ProbesSelector {
	return []manager.ProbesSelector{
		&manager.AllOf{Selectors: []manager.ProbesSelector{
			hookFunc("hook_rtnl_create_link"),
		}},
	}
}

// NetworkSelectors is the list of probes that should be activated when the network is enabled
func NetworkSelectors() []manager.ProbesSelector {
	return []manager.ProbesSelector{
		// flow classification probes
		&manager.AllOf{Selectors: []manager.ProbesSelector{
			hookFunc("hook_accept"),
			hookFunc("hook_security_socket_bind"),
			hookFunc("hook_security_socket_connect"),
			hookFunc("hook_security_sk_classify_flow"),
			hookFunc("hook_inet_release"),
			hookFunc("hook_inet_csk_destroy_sock"),
			hookFunc("hook_sk_destruct"),
			hookFunc("hook_inet_put_port"),
			hookFunc("hook_inet_shutdown"),
			hookFunc("hook_inet_bind"),
			hookFunc("rethook_inet_bind"),
			hookFunc("hook_inet6_bind"),
			hookFunc("rethook_inet6_bind"),
			hookFunc("hook_sk_common_release"),
			hookFunc("hook_path_get"),
			hookFunc("hook_proc_fd_link"),
		}},

		// network device probes
		&manager.AllOf{Selectors: []manager.ProbesSelector{
			hookFunc("hook_register_netdevice"),
			hookFunc("rethook_register_netdevice"),
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_dev_change_net_namespace"),
				hookFunc("hook___dev_change_net_namespace"),
			}},
		}},
		&manager.BestEffort{Selectors: []manager.ProbesSelector{
			hookFunc("hook_dev_get_valid_name"),
			hookFunc("hook_dev_new_index"),
			hookFunc("rethook_dev_new_index"),
			hookFunc("hook___dev_get_by_index"),
		}},
	}
}

// SyscallMonitorSelectors is the list of probes that should be activated for the syscall monitor feature
func SyscallMonitorSelectors() []manager.ProbesSelector {
	return []manager.ProbesSelector{
		&manager.ProbeSelector{
			ProbeIdentificationPair: manager.ProbeIdentificationPair{
				UID:          SecurityAgentUID,
				EBPFFuncName: "sys_enter",
			},
		},
	}
}

// SnapshotSelectors selectors required during the snapshot
func SnapshotSelectors(fentry bool) []manager.ProbesSelector {
	procsOpen := hookFunc("hook_cgroup_procs_open")
	tasksOpen := hookFunc("hook_cgroup_tasks_open")
	return []manager.ProbesSelector{
		&manager.BestEffort{Selectors: []manager.ProbesSelector{procsOpen, tasksOpen}},

		// required to stat /proc/.../exe
		hookFunc("hook_security_inode_getattr"),
		&manager.AllOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "newfstatat", fentry, EntryAndExit)},
	}
}

// GetSelectorsPerEventType returns the list of probes that should be activated for each event
func GetSelectorsPerEventType(fentry bool) map[eval.EventType][]manager.ProbesSelector {
	selectorsPerEventTypeStore := map[eval.EventType][]manager.ProbesSelector{
		// The following probes will always be activated, regardless of the loaded rules
		"*": {
			// Exec probes
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				&manager.ProbeSelector{ProbeIdentificationPair: manager.ProbeIdentificationPair{UID: SecurityAgentUID, EBPFFuncName: "sched_process_fork"}},
				hookFunc("hook_do_exit"),
				&manager.BestEffort{Selectors: []manager.ProbesSelector{
					hookFunc("hook_prepare_binprm"),
					hookFunc("hook_bprm_execve"),
					hookFunc("hook_security_bprm_check"),
				}},
				hookFunc("hook_setup_new_exec_interp"),
				// kernels < 4.17 will rely on the tracefs events interface to attach kprobes, which requires event names to be unique
				// because the setup_new_exec_interp and setup_new_exec_args_envs probes are attached to the same function, we rely on using a secondary uid for that purpose
				hookFunc("hook_setup_new_exec_args_envs", withUID(SecurityAgentUID+"_a")),
				hookFunc("hook_setup_arg_pages"),
				hookFunc("hook_mprotect_fixup"),
				hookFunc("hook_exit_itimers"),
				hookFunc("hook_do_dentry_open"),
				hookFunc("hook_vfs_open"),
				hookFunc("hook_commit_creds"),
				hookFunc("hook_switch_task_namespaces"),
				hookFunc("hook_do_coredump"),
				hookFunc("hook_audit_set_loginuid"),
				hookFunc("rethook_audit_set_loginuid"),
			}},
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_cgroup_procs_write"),
				hookFunc("hook_cgroup1_procs_write"),
			}},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_cgroup_procs_open"),
			}},
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook__do_fork"),
				hookFunc("hook_do_fork"),
				hookFunc("hook_kernel_clone"),
				hookFunc("hook_kernel_thread"),
				hookFunc("hook_user_mode_thread"),
			}},
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_cgroup_tasks_write"),
				hookFunc("hook_cgroup1_tasks_write"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "execve", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "execveat", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setuid", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setuid16", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setgid", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setgid16", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setfsuid", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setfsuid16", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setfsgid", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setfsgid16", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setreuid", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setreuid16", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setregid", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setregid16", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setresuid", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setresuid16", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setresgid", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setresgid16", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "capset", fentry, EntryAndExit)},

			// File Attributes
			hookFunc("hook_security_inode_setattr"),

			// Open probes
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_security_path_truncate"),
				hookFunc("hook_security_file_truncate"),
				hookFunc("hook_vfs_truncate"),
				hookFunc("hook_do_truncate"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "open", fentry, EntryAndExit, true)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "creat", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "truncate", fentry, EntryAndExit, true)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "ftruncate", fentry, EntryAndExit, true)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "openat", fentry, EntryAndExit, true)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "openat2", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "open_by_handle_at", fentry, EntryAndExit, true)},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_io_openat"),
				hookFunc("hook_io_openat2"),
				hookFunc("rethook_io_openat2"),
			}},
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_filp_close"),
			}},
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_terminate_walk"),
			}},

			// iouring
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				&manager.ProbeSelector{ProbeIdentificationPair: manager.ProbeIdentificationPair{UID: SecurityAgentUID, EBPFFuncName: "io_uring_create"}},
				&manager.OneOf{Selectors: []manager.ProbesSelector{
					hookFunc("hook_io_allocate_scq_urings"),
					hookFunc("hook_io_sq_offload_start"),
					hookFunc("rethook_io_ring_ctx_alloc"),
				}},
			}},

			// Mount probes
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_attach_recursive_mnt"),
				hookFunc("hook_propagate_mnt"),
				hookFunc("hook_security_sb_umount"),
				hookFunc("hook_clone_mnt"),
				hookFunc("rethook_clone_mnt"),
			}},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("rethook_alloc_vfsmnt"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "mount", fentry, EntryAndExit, true)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "fsmount", fentry, EntryAndExit, false)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "open_tree", fentry, EntryAndExit, false)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "umount", fentry, Exit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "unshare", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_attach_mnt"),
				hookFunc("hook___attach_mnt"),
				hookFunc("hook_mnt_set_mountpoint"),
			}},

			// Rename probes
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_vfs_rename"),
				hookFunc("hook_mnt_want_write"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "rename", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "renameat", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: append(
				[]manager.ProbesSelector{
					hookFunc("hook_do_renameat2"),
					hookFunc("rethook_do_renameat2"),
				},
				ExpandSyscallProbesSelector(SecurityAgentUID, "renameat2", fentry, EntryAndExit)...)},

			// unlink rmdir probes
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_mnt_want_write"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "unlinkat", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_do_unlinkat"),
				hookFunc("rethook_do_unlinkat"),
			}},

			// Rmdir probes
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_security_inode_rmdir"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "rmdir", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_do_rmdir"),
				hookFunc("rethook_do_rmdir"),
			}},

			// Unlink probes
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_vfs_unlink"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "unlink", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_do_linkat"),
				hookFunc("rethook_do_linkat"),
			}},

			// ioctl probes
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_do_vfs_ioctl"),
			}},

			// Link
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				// source dentry
				hookFunc("hook_complete_walk"),
				// target dentry
				&manager.OneOf{Selectors: []manager.ProbesSelector{
					hookFunc("rethook_filename_create"),
					hookFunc("rethook___lookup_hash"),
				}},
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "link", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "linkat", fentry, EntryAndExit)},

			// selinux
			// This needs to be best effort, as sel_write_disable is in the process of being removed
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_sel_write_disable"),
				hookFunc("hook_sel_write_enforce"),
				hookFunc("hook_sel_write_bool"),
				hookFunc("hook_sel_commit_bools_write"),
			}}},

		// List of probes required to capture chmod events
		"chmod": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_mnt_want_write"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "chmod", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "fchmod", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "fchmodat", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "fchmodat2", fentry, EntryAndExit)},
		},

		// List of probes required to capture chown events
		"chown": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_mnt_want_write"),
			}},
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_mnt_want_write_file"),
				hookFunc("hook_mnt_want_write_file_path"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "chown", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "chown16", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "fchown", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "fchown16", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "fchownat", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "lchown", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "lchown16", fentry, EntryAndExit)},
		},

		// List of probes required to capture mkdir events
		"mkdir": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_vfs_mkdir"),
				&manager.OneOf{Selectors: []manager.ProbesSelector{
					hookFunc("hook_filename_create"),
					hookFunc("hook_security_path_mkdir"),
				}},
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "mkdir", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "mkdirat", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_do_mkdirat"),
				hookFunc("rethook_do_mkdirat"),
			}}},

		// List of probes required to capture removexattr events
		"removexattr": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_vfs_removexattr"),
				hookFunc("hook_mnt_want_write"),
			}},
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_mnt_want_write_file"),
				hookFunc("hook_mnt_want_write_file_path"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "removexattr", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "fremovexattr", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "lremovexattr", fentry, EntryAndExit)},
		},

		// List of probes required to capture setxattr events
		"setxattr": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_vfs_setxattr"),
				hookFunc("hook_mnt_want_write"),
			}},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_io_fsetxattr"),
				hookFunc("rethook_io_fsetxattr"),
				hookFunc("hook_io_setxattr"),
				hookFunc("rethook_io_setxattr"),
			}},
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_mnt_want_write_file"),
				hookFunc("hook_mnt_want_write_file_path"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setxattr", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "fsetxattr", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "lsetxattr", fentry, EntryAndExit)},
		},

		// List of probes required to capture utimes events
		"utimes": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_mnt_want_write"),
			}},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "utime", fentry, EntryAndExit, true)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "utime32", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "utimes", fentry, EntryAndExit, true)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "utimes", fentry, EntryAndExit|ExpandTime32)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "utimensat", fentry, EntryAndExit, true)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "utimensat", fentry, EntryAndExit|ExpandTime32)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "futimesat", fentry, EntryAndExit, true)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "futimesat", fentry, EntryAndExit|ExpandTime32)},
		},

		// List of probes required to capture bpf events
		"bpf": {
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_security_bpf_map"),
				hookFunc("hook_security_bpf_prog"),
				hookFunc("hook_check_helper_call"),
			}},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "bpf", fentry, EntryAndExit)},
		},

		// List of probes required to capture ptrace events
		"ptrace": {
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "ptrace", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_ptrace_check_attach"),
				hookFunc("hook_arch_ptrace"),
			}},
		},

		// List of probes required to capture mmap events
		"mmap": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_vm_mmap_pgoff"),
				hookFunc("rethook_vm_mmap_pgoff"),
				hookFunc("hook_security_mmap_file"),
			}},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_get_unmapped_area"),
			}},
		},

		// List of probes required to capture mprotect events
		"mprotect": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_security_file_mprotect"),
			}},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "mprotect", fentry, EntryAndExit)},
		},

		// List of probes required to capture kernel load_module events
		"load_module": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				&manager.OneOf{Selectors: []manager.ProbesSelector{
					hookFunc("hook_security_kernel_read_file"),
					hookFunc("hook_security_kernel_module_from_file"),
				}},
				&manager.OneOf{Selectors: []manager.ProbesSelector{
					hookFunc("hook_mod_sysfs_setup"),
					hookFunc("hook_module_param_sysfs_setup"),
				}},
			}},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "init_module", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "finit_module", fentry, EntryAndExit)},
		},

		// List of probes required to capture kernel unload_module events
		"unload_module": {
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "delete_module", fentry, EntryAndExit)},
		},

		// List of probes required to capture signal events
		"signal": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("rethook_check_kill_permission"),
				hookFunc("hook_check_kill_permission"),
			}},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "kill", fentry, Entry)},
		},

		// List of probes required to capture setsockopt events
		"setsockopt": {
			&manager.AllOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setsockopt", fentry, EntryAndExit)},
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_security_socket_setsockopt"),
				hookFunc("hook_sk_attach_filter"),
				hookFunc("hook_release_sock"),
				hookFunc("rethook_release_sock"),
			}},
		},

		// List of probes required to capture splice events
		"splice": {
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "splice", fentry, EntryAndExit)},
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_get_pipe_info"),
				hookFunc("rethook_get_pipe_info"),
			}}},

		// List of probes required to capture accept events
		"accept": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_accept"),
			}},
		},
		// List of probes required to capture bind events
		"bind": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_security_socket_bind"),
			}},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_io_bind"),
				hookFunc("rethook_io_bind"),
			}},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "bind", fentry, EntryAndExit)},
		},
		// List of probes required to capture connect events
		"connect": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_security_socket_connect"),
			}},
			&manager.BestEffort{Selectors: []manager.ProbesSelector{
				hookFunc("hook_io_connect"),
				hookFunc("rethook_io_connect"),
			}},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "connect", fentry, EntryAndExit)},
		},

		// List of probes required to capture chdir events
		"chdir": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_set_fs_pwd"),
			}},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "chdir", fentry, EntryAndExit)},
			&manager.OneOf{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "fchdir", fentry, EntryAndExit)},
		},

		// List of probes required to capture network_flow_monitor events
		"network_flow_monitor": {
			// perf_event probes
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				&manager.ProbeSelector{
					ProbeIdentificationPair: manager.ProbeIdentificationPair{
						UID:          SecurityAgentUID,
						EBPFFuncName: "network_stats_worker",
					},
				},
			}},
		},

		// List of probes required to capture sysctl events
		"sysctl": {
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				&manager.ProbeSelector{
					ProbeIdentificationPair: manager.ProbeIdentificationPair{
						UID:          SecurityAgentUID,
						EBPFFuncName: SysCtlProbeFunctionName,
					},
				},
				hookFunc("hook_proc_sys_call_handler"),
			}},
		},
		"setrlimit": {
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "setrlimit", fentry, EntryAndExit)},
			&manager.BestEffort{Selectors: ExpandSyscallProbesSelector(SecurityAgentUID, "prlimit64", fentry, EntryAndExit)},
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				hookFunc("hook_security_task_setrlimit"),
			}},
		},
	}

	// Add probes required to track network interfaces and map network flows to processes
	// networkEventTypes: dns, imds, packet, network_monitor
	networkEventTypes := model.GetEventTypePerCategory(model.NetworkCategory)[model.NetworkCategory]
	for _, networkEventType := range networkEventTypes {
		selectorsPerEventTypeStore[networkEventType] = []manager.ProbesSelector{
			&manager.AllOf{Selectors: []manager.ProbesSelector{
				&manager.AllOf{Selectors: NetworkSelectors()},
				&manager.AllOf{Selectors: NetworkVethSelectors()},
			}},
		}
	}

	// add probes depending on loaded modules
	loadedModules, err := utils.FetchLoadedModules()
	if err == nil {
		if _, ok := loadedModules["nf_nat"]; ok {
			for _, networkEventType := range networkEventTypes {
				selectorsPerEventTypeStore[networkEventType] = append(selectorsPerEventTypeStore[networkEventType], NetworkNFNatSelectors()...)
			}
		}
	}

	if ShouldUseModuleLoadTracepoint() {
		selectorsPerEventTypeStore["load_module"] = append(selectorsPerEventTypeStore["load_module"], &manager.BestEffort{Selectors: []manager.ProbesSelector{
			&manager.ProbeSelector{ProbeIdentificationPair: manager.ProbeIdentificationPair{UID: SecurityAgentUID, EBPFFuncName: "module_load"}},
		}})
	}

	return selectorsPerEventTypeStore
}
