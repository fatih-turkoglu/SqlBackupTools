export namespace app {
	
	export class AppInfo {
	    version: string;
	    dataDir: string;
	    jobsDir: string;
	    logsDir: string;
	    elevated: boolean;
	    startupError: string;
	
	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.dataDir = source["dataDir"];
	        this.jobsDir = source["jobsDir"];
	        this.logsDir = source["logsDir"];
	        this.elevated = source["elevated"];
	        this.startupError = source["startupError"];
	    }
	}
	export class ConnectionInfo {
	    server: mssql.ServerInfo;
	    supportsCompression: boolean;
	    protocolLabel: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = this.convertValues(source["server"], mssql.ServerInfo);
	        this.supportsCompression = source["supportsCompression"];
	        this.protocolLabel = source["protocolLabel"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConnectionRequest {
	    connection: config.Connection;
	    password: string;
	    passwordChanged: boolean;
	    storedJob: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection = this.convertValues(source["connection"], config.Connection);
	        this.password = source["password"];
	        this.passwordChanged = source["passwordChanged"];
	        this.storedJob = source["storedJob"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JobDetail {
	    job?: config.Job;
	    hasPassword: boolean;
	    passwordError: string;
	
	    static createFrom(source: any = {}) {
	        return new JobDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.job = this.convertValues(source["job"], config.Job);
	        this.hasPassword = source["hasPassword"];
	        this.passwordError = source["passwordError"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TaskInfo {
	    status: scheduler.TaskStatus;
	    outdated: boolean;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new TaskInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = this.convertValues(source["status"], scheduler.TaskStatus);
	        this.outdated = source["outdated"];
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RunSummary {
	    path: string;
	    job: string;
	    // Go type: time
	    start: any;
	    elapsedMs: number;
	    exitCode: number;
	    succeeded: number;
	    failed: number;
	    skipped: number;
	    error: string;
	    alreadyRunning: boolean;
	    readError: string;
	
	    static createFrom(source: any = {}) {
	        return new RunSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.job = source["job"];
	        this.start = this.convertValues(source["start"], null);
	        this.elapsedMs = source["elapsedMs"];
	        this.exitCode = source["exitCode"];
	        this.succeeded = source["succeeded"];
	        this.failed = source["failed"];
	        this.skipped = source["skipped"];
	        this.error = source["error"];
	        this.alreadyRunning = source["alreadyRunning"];
	        this.readError = source["readError"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JobSummary {
	    name: string;
	    server: string;
	    databases: string[];
	    includeNewDatabases: boolean;
	    schedule: config.Schedule;
	    running: boolean;
	    lastRun?: RunSummary;
	    task?: TaskInfo;
	    errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new JobSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.server = source["server"];
	        this.databases = source["databases"];
	        this.includeNewDatabases = source["includeNewDatabases"];
	        this.schedule = this.convertValues(source["schedule"], config.Schedule);
	        this.running = source["running"];
	        this.lastRun = this.convertValues(source["lastRun"], RunSummary);
	        this.task = this.convertValues(source["task"], TaskInfo);
	        this.errors = source["errors"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JobList {
	    jobs: JobSummary[];
	    errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new JobList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.jobs = this.convertValues(source["jobs"], JobSummary);
	        this.errors = source["errors"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class LanguageState {
	    current: string;
	    languages: i18n.LanguageInfo[];
	
	    static createFrom(source: any = {}) {
	        return new LanguageState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.languages = this.convertValues(source["languages"], i18n.LanguageInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LogText {
	    text: string;
	    size: number;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LogText(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.size = source["size"];
	        this.truncated = source["truncated"];
	    }
	}
	
	export class SaveJobRequest {
	    job: config.Job;
	    originalName: string;
	    password: string;
	    passwordChanged: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SaveJobRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.job = this.convertValues(source["job"], config.Job);
	        this.originalName = source["originalName"];
	        this.password = source["password"];
	        this.passwordChanged = source["passwordChanged"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SaveJobResult {
	    saved: boolean;
	    errors: string[];
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new SaveJobResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saved = source["saved"];
	        this.errors = source["errors"];
	        this.warnings = source["warnings"];
	    }
	}
	export class TaskActionResult {
	    elevated: boolean;
	    task: TaskInfo;
	
	    static createFrom(source: any = {}) {
	        return new TaskActionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.elevated = source["elevated"];
	        this.task = this.convertValues(source["task"], TaskInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace backup {
	
	export class ArchiveResult {
	    file: string;
	    databases: string[];
	    sizeBytes: number;
	    sourceBytes: number;
	    bakDeleted: boolean;
	    elapsedMs: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ArchiveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.databases = source["databases"];
	        this.sizeBytes = source["sizeBytes"];
	        this.sourceBytes = source["sourceBytes"];
	        this.bakDeleted = source["bakDeleted"];
	        this.elapsedMs = source["elapsedMs"];
	        this.error = source["error"];
	    }
	}
	export class DBResult {
	    database: string;
	    status: string;
	    file?: string;
	    sizeBytes: number;
	    fileBytes: number;
	    compressed: boolean;
	    verified: boolean;
	    // Go type: time
	    start: any;
	    // Go type: time
	    end: any;
	    elapsedMs: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DBResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.database = source["database"];
	        this.status = source["status"];
	        this.file = source["file"];
	        this.sizeBytes = source["sizeBytes"];
	        this.fileBytes = source["fileBytes"];
	        this.compressed = source["compressed"];
	        this.verified = source["verified"];
	        this.start = this.convertValues(source["start"], null);
	        this.end = this.convertValues(source["end"], null);
	        this.elapsedMs = source["elapsedMs"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DeletedFile {
	    file: string;
	    bytes: number;
	    // Go type: time
	    time: any;
	
	    static createFrom(source: any = {}) {
	        return new DeletedFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.bytes = source["bytes"];
	        this.time = this.convertValues(source["time"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RetentionResult {
	    mode: string;
	    value: number;
	    deleted: DeletedFile[];
	    elapsedMs: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RetentionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.value = source["value"];
	        this.deleted = this.convertValues(source["deleted"], DeletedFile);
	        this.elapsedMs = source["elapsedMs"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result {
	    job: string;
	    server: string;
	    edition?: string;
	    protocol?: string;
	    // Go type: time
	    start: any;
	    // Go type: time
	    end: any;
	    elapsedMs: number;
	    databases: DBResult[];
	    archives: ArchiveResult[];
	    retention?: RetentionResult;
	    warnings: string[];
	    error?: string;
	    alreadyRunning?: boolean;
	    exitCode: number;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.job = source["job"];
	        this.server = source["server"];
	        this.edition = source["edition"];
	        this.protocol = source["protocol"];
	        this.start = this.convertValues(source["start"], null);
	        this.end = this.convertValues(source["end"], null);
	        this.elapsedMs = source["elapsedMs"];
	        this.databases = this.convertValues(source["databases"], DBResult);
	        this.archives = this.convertValues(source["archives"], ArchiveResult);
	        this.retention = this.convertValues(source["retention"], RetentionResult);
	        this.warnings = source["warnings"];
	        this.error = source["error"];
	        this.alreadyRunning = source["alreadyRunning"];
	        this.exitCode = source["exitCode"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace config {
	
	export class Archive {
	    zipEnabled: boolean;
	    deleteBakAfterZip: boolean;
	    zipPerDatabase: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Archive(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.zipEnabled = source["zipEnabled"];
	        this.deleteBakAfterZip = source["deleteBakAfterZip"];
	        this.zipPerDatabase = source["zipPerDatabase"];
	    }
	}
	export class Backup {
	    serverPath: string;
	    accessPath: string;
	    fileNameTemplate: string;
	    compression: boolean;
	    checksum: boolean;
	    copyOnly: boolean;
	    verify: boolean;
	    commandTimeoutMin: number;
	
	    static createFrom(source: any = {}) {
	        return new Backup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverPath = source["serverPath"];
	        this.accessPath = source["accessPath"];
	        this.fileNameTemplate = source["fileNameTemplate"];
	        this.compression = source["compression"];
	        this.checksum = source["checksum"];
	        this.copyOnly = source["copyOnly"];
	        this.verify = source["verify"];
	        this.commandTimeoutMin = source["commandTimeoutMin"];
	    }
	}
	export class Connection {
	    server: string;
	    instance: string;
	    port: number;
	    protocol: string;
	    auth: string;
	    user: string;
	    passwordEnc: string;
	    encrypt: boolean;
	    trustServerCert: boolean;
	    timeoutSec: number;
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = source["server"];
	        this.instance = source["instance"];
	        this.port = source["port"];
	        this.protocol = source["protocol"];
	        this.auth = source["auth"];
	        this.user = source["user"];
	        this.passwordEnc = source["passwordEnc"];
	        this.encrypt = source["encrypt"];
	        this.trustServerCert = source["trustServerCert"];
	        this.timeoutSec = source["timeoutSec"];
	    }
	}
	export class Schedule {
	    frequency: string;
	    time: string;
	    days: string[];
	    intervalHours: number;
	    runAs: string;
	
	    static createFrom(source: any = {}) {
	        return new Schedule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.frequency = source["frequency"];
	        this.time = source["time"];
	        this.days = source["days"];
	        this.intervalHours = source["intervalHours"];
	        this.runAs = source["runAs"];
	    }
	}
	export class Retention {
	    mode: string;
	    value: number;
	
	    static createFrom(source: any = {}) {
	        return new Retention(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.value = source["value"];
	    }
	}
	export class Job {
	    version: number;
	    name: string;
	    connection: Connection;
	    databases: string[];
	    excludedDatabases: string[];
	    includeNewDatabases: boolean;
	    excludeSystemDatabases: boolean;
	    backup: Backup;
	    archive: Archive;
	    retention: Retention;
	    schedule: Schedule;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.name = source["name"];
	        this.connection = this.convertValues(source["connection"], Connection);
	        this.databases = source["databases"];
	        this.excludedDatabases = source["excludedDatabases"];
	        this.includeNewDatabases = source["includeNewDatabases"];
	        this.excludeSystemDatabases = source["excludeSystemDatabases"];
	        this.backup = this.convertValues(source["backup"], Backup);
	        this.archive = this.convertValues(source["archive"], Archive);
	        this.retention = this.convertValues(source["retention"], Retention);
	        this.schedule = this.convertValues(source["schedule"], Schedule);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class Settings {
	    language: string;
	    logKeepDays: number;
	    theme: string;
	    defaultServerPath: string;
	    defaultAccessPath: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.logKeepDays = source["logKeepDays"];
	        this.theme = source["theme"];
	        this.defaultServerPath = source["defaultServerPath"];
	        this.defaultAccessPath = source["defaultAccessPath"];
	    }
	}

}

export namespace i18n {
	
	export class LanguageInfo {
	    code: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new LanguageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.name = source["name"];
	    }
	}

}

export namespace logging {
	
	export class LogInfo {
	    path: string;
	    job: string;
	    // Go type: time
	    date: any;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new LogInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.job = source["job"];
	        this.date = this.convertValues(source["date"], null);
	        this.size = source["size"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace mssql {
	
	export class Database {
	    name: string;
	    state: string;
	    recoveryModel: string;
	    sizeMB: number;
	    isSystem: boolean;
	    isReadOnly: boolean;
	    canBackup: boolean;
	    // Go type: time
	    lastFullBackup?: any;
	
	    static createFrom(source: any = {}) {
	        return new Database(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.state = source["state"];
	        this.recoveryModel = source["recoveryModel"];
	        this.sizeMB = source["sizeMB"];
	        this.isSystem = source["isSystem"];
	        this.isReadOnly = source["isReadOnly"];
	        this.canBackup = source["canBackup"];
	        this.lastFullBackup = this.convertValues(source["lastFullBackup"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ServerInfo {
	    serverName: string;
	    machineName: string;
	    productVersion: string;
	    productLevel: string;
	    edition: string;
	    engineEdition: number;
	    isSysAdmin: boolean;
	    loginName: string;
	    defaultBackupDir: string;
	    protocol: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverName = source["serverName"];
	        this.machineName = source["machineName"];
	        this.productVersion = source["productVersion"];
	        this.productLevel = source["productLevel"];
	        this.edition = source["edition"];
	        this.engineEdition = source["engineEdition"];
	        this.isSysAdmin = source["isSysAdmin"];
	        this.loginName = source["loginName"];
	        this.defaultBackupDir = source["defaultBackupDir"];
	        this.protocol = source["protocol"];
	    }
	}

}

export namespace scheduler {
	
	export class TaskDefinition {
	    runAs: string;
	    user: string;
	    command: string;
	    arguments: string;
	    schedule: config.Schedule;
	
	    static createFrom(source: any = {}) {
	        return new TaskDefinition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runAs = source["runAs"];
	        this.user = source["user"];
	        this.command = source["command"];
	        this.arguments = source["arguments"];
	        this.schedule = this.convertValues(source["schedule"], config.Schedule);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TaskStatus {
	    exists: boolean;
	    path: string;
	    state?: string;
	    enabled: boolean;
	    // Go type: time
	    lastRun?: any;
	    lastResult: number;
	    // Go type: time
	    nextRun?: any;
	    definition: TaskDefinition;
	
	    static createFrom(source: any = {}) {
	        return new TaskStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exists = source["exists"];
	        this.path = source["path"];
	        this.state = source["state"];
	        this.enabled = source["enabled"];
	        this.lastRun = this.convertValues(source["lastRun"], null);
	        this.lastResult = source["lastResult"];
	        this.nextRun = this.convertValues(source["nextRun"], null);
	        this.definition = this.convertValues(source["definition"], TaskDefinition);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

