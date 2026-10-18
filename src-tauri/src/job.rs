//! Windows Job Object fallback.
//!
//! The shell normally kills the server on exit, but a force-killed shell (Task
//! Manager, crash, debugger stop) leaves the child running. Assigning the child
//! to a job with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` makes the OS terminate it
//! once our handle goes away, which happens even on an abnormal exit.

#[cfg(windows)]
mod platform {
    use std::ffi::c_void;
    use std::sync::Mutex;

    use windows_sys::Win32::Foundation::{CloseHandle, HANDLE};
    use windows_sys::Win32::System::JobObjects::{
        AssignProcessToJobObject, CreateJobObjectW, JobObjectExtendedLimitInformation,
        SetInformationJobObject, JOBOBJECT_EXTENDED_LIMIT_INFORMATION,
        JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
    };
    use windows_sys::Win32::System::Threading::{
        OpenProcess, PROCESS_SET_QUOTA, PROCESS_TERMINATE,
    };

    static JOB: Mutex<isize> = Mutex::new(0);

    pub fn attach(pid: u32) -> Result<(), String> {
        unsafe {
            let job = CreateJobObjectW(std::ptr::null(), std::ptr::null());
            if job.is_null() {
                return Err("CreateJobObjectW failed".into());
            }

            let mut limits: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = std::mem::zeroed();
            limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
            let size = std::mem::size_of::<JOBOBJECT_EXTENDED_LIMIT_INFORMATION>() as u32;
            if SetInformationJobObject(
                job,
                JobObjectExtendedLimitInformation,
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
                CloseHandle(previous as HANDLE);
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
