//! Windows Job Object fallback.
//!
//! The shell normally kills the server on exit, but a force-killed shell (Task
//! Manager, crash, debugger stop) leaves the child running. Assigning the child
//! to a job with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` makes the OS terminate it
//! once our handle goes away, which happens even on an abnormal exit.
//!
//! The Win32 calls are declared here instead of pulling in `windows-sys`: the
//! crate only needs five functions and one struct, and the wrapper crate's
//! module layout differs between versions.

#[cfg(windows)]
mod platform {
    use std::ffi::c_void;
    use std::sync::Mutex;

    #[link(name = "kernel32")]
    #[allow(non_snake_case)]
    extern "system" {
        fn CreateJobObjectW(attributes: *const c_void, name: *const u16) -> *mut c_void;
        fn SetInformationJobObject(
            job: *mut c_void,
            class: i32,
            info: *const c_void,
            length: u32,
        ) -> i32;
        fn AssignProcessToJobObject(job: *mut c_void, process: *mut c_void) -> i32;
        fn OpenProcess(access: u32, inherit: i32, pid: u32) -> *mut c_void;
        fn CloseHandle(handle: *mut c_void) -> i32;
    }

    const JOB_OBJECT_EXTENDED_LIMIT_INFORMATION: i32 = 9;
    const JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE: u32 = 0x0000_2000;
    const PROCESS_SET_QUOTA: u32 = 0x0100;
    const PROCESS_TERMINATE: u32 = 0x0001;

    #[repr(C)]
    struct BasicLimitInformation {
        per_process_user_time_limit: i64,
        per_job_user_time_limit: i64,
        limit_flags: u32,
        minimum_working_set_size: usize,
        maximum_working_set_size: usize,
        active_process_limit: u32,
        affinity: usize,
        priority_class: u32,
        scheduling_class: u32,
    }

    #[repr(C)]
    struct IoCounters {
        read_operation_count: u64,
        write_operation_count: u64,
        other_operation_count: u64,
        read_transfer_count: u64,
        write_transfer_count: u64,
        other_transfer_count: u64,
    }

    #[repr(C)]
    struct ExtendedLimitInformation {
        basic: BasicLimitInformation,
        io: IoCounters,
        process_memory_limit: usize,
        job_memory_limit: usize,
        peak_process_memory_used: usize,
        peak_job_memory_used: usize,
    }

    static JOB: Mutex<isize> = Mutex::new(0);

    pub fn attach(pid: u32) -> Result<(), String> {
        unsafe {
            let job = CreateJobObjectW(std::ptr::null(), std::ptr::null());
            if job.is_null() {
                return Err("CreateJobObjectW failed".into());
            }

            let limits = ExtendedLimitInformation {
                basic: BasicLimitInformation {
                    per_process_user_time_limit: 0,
                    per_job_user_time_limit: 0,
                    limit_flags: JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
                    minimum_working_set_size: 0,
                    maximum_working_set_size: 0,
                    active_process_limit: 0,
                    affinity: 0,
                    priority_class: 0,
                    scheduling_class: 0,
                },
                io: IoCounters {
                    read_operation_count: 0,
                    write_operation_count: 0,
                    other_operation_count: 0,
                    read_transfer_count: 0,
                    write_transfer_count: 0,
                    other_transfer_count: 0,
                },
                process_memory_limit: 0,
                job_memory_limit: 0,
                peak_process_memory_used: 0,
                peak_job_memory_used: 0,
            };
            let size = std::mem::size_of::<ExtendedLimitInformation>() as u32;
            if SetInformationJobObject(
                job,
                JOB_OBJECT_EXTENDED_LIMIT_INFORMATION,
                &limits as *const _ as *const c_void,
                size,
            ) == 0
            {
                CloseHandle(job);
                return Err("SetInformationJobObject failed".into());
            }

            let process = OpenProcess(PROCESS_SET_QUOTA | PROCESS_TERMINATE, 0, pid);
            if process.is_null() {
                CloseHandle(job);
                return Err(format!("OpenProcess failed for pid {pid}"));
            }
            let assigned = AssignProcessToJobObject(job, process);
            CloseHandle(process);
            if assigned == 0 {
                CloseHandle(job);
                return Err("AssignProcessToJobObject failed".into());
            }

            // Keep the handle: closing it is what terminates the child.
            let mut slot = JOB.lock().expect("job lock");
            let previous = std::mem::replace(&mut *slot, job as isize);
            if previous != 0 {
                CloseHandle(previous as *mut c_void);
            }
            Ok(())
        }
    }
}

#[cfg(not(windows))]
mod platform {
    pub fn attach(_pid: u32) -> Result<(), String> {
        Ok(())
    }
}

pub fn attach(pid: u32) {
    match platform::attach(pid) {
        Ok(()) => crate::log::info(format!("server pid {pid} assigned to a kill-on-close job")),
        Err(err) => crate::log::error(format!("cannot attach pid {pid} to a job object: {err}")),
    }
}
