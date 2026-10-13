export namespace app {
	
	export class GraphInfo {
	    rows: number;
	    done: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GraphInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows = source["rows"];
	        this.done = source["done"];
	    }
	}
	export class RowDTO {
	    hash: string;
	    parents: string[];
	    kind: string;
	    lane: number;
	    color: number;
	    up: number[][];
	    down: number[][];
	    author: string;
	    email: string;
	    time: number;
	    subject: string;
	    stashRef: string;
	
	    static createFrom(source: any = {}) {
	        return new RowDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.parents = source["parents"];
	        this.kind = source["kind"];
	        this.lane = source["lane"];
	        this.color = source["color"];
	        this.up = source["up"];
	        this.down = source["down"];
	        this.author = source["author"];
	        this.email = source["email"];
	        this.time = source["time"];
	        this.subject = source["subject"];
	        this.stashRef = source["stashRef"];
	    }
	}
	export class TabInfo {
	    id: string;
	    path: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new TabInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.name = source["name"];
	    }
	}

}

export namespace diff {
	
	export class Line {
	    kind: string;
	    oldNo: number;
	    newNo: number;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new Line(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.oldNo = source["oldNo"];
	        this.newNo = source["newNo"];
	        this.text = source["text"];
	    }
	}
	export class Hunk {
	    oldStart: number;
	    oldLines: number;
	    newStart: number;
	    newLines: number;
	    section: string;
	    lines: Line[];
	
	    static createFrom(source: any = {}) {
	        return new Hunk(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.oldStart = source["oldStart"];
	        this.oldLines = source["oldLines"];
	        this.newStart = source["newStart"];
	        this.newLines = source["newLines"];
	        this.section = source["section"];
	        this.lines = this.convertValues(source["lines"], Line);
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
	export class FileDiff {
	    oldPath: string;
	    newPath: string;
	    status: string;
	    binary: boolean;
	    oldMode: string;
	    newMode: string;
	    similarity: number;
	    hunks: Hunk[];
	
	    static createFrom(source: any = {}) {
	        return new FileDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.oldPath = source["oldPath"];
	        this.newPath = source["newPath"];
	        this.status = source["status"];
	        this.binary = source["binary"];
	        this.oldMode = source["oldMode"];
	        this.newMode = source["newMode"];
	        this.similarity = source["similarity"];
	        this.hunks = this.convertValues(source["hunks"], Hunk);
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

export namespace gitread {
	
	export class FileChange {
	    path: string;
	    oldPath: string;
	    status: string;
	    additions: number;
	    deletions: number;
	    binary: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.status = source["status"];
	        this.additions = source["additions"];
	        this.deletions = source["deletions"];
	        this.binary = source["binary"];
	    }
	}
	export class CommitDetail {
	    hash: string;
	    summary: string;
	    body: string;
	    parents: string[];
	    author: string;
	    authorEmail: string;
	    authorTime: number;
	    committer: string;
	    committerEmail: string;
	    commitTime: number;
	    files: FileChange[];
	
	    static createFrom(source: any = {}) {
	        return new CommitDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.summary = source["summary"];
	        this.body = source["body"];
	        this.parents = source["parents"];
	        this.author = source["author"];
	        this.authorEmail = source["authorEmail"];
	        this.authorTime = source["authorTime"];
	        this.committer = source["committer"];
	        this.committerEmail = source["committerEmail"];
	        this.commitTime = source["commitTime"];
	        this.files = this.convertValues(source["files"], FileChange);
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
	
	export class Head {
	    branch: string;
	    detached: boolean;
	    unborn: boolean;
	    hash: string;
	
	    static createFrom(source: any = {}) {
	        return new Head(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.detached = source["detached"];
	        this.unborn = source["unborn"];
	        this.hash = source["hash"];
	    }
	}
	export class Ref {
	    name: string;
	    short: string;
	    kind: string;
	    remote: string;
	    hash: string;
	    upstream: string;
	    ahead: number;
	    behind: number;
	    gone: boolean;
	    isHead: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Ref(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.short = source["short"];
	        this.kind = source["kind"];
	        this.remote = source["remote"];
	        this.hash = source["hash"];
	        this.upstream = source["upstream"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.gone = source["gone"];
	        this.isHead = source["isHead"];
	    }
	}
	export class Refs {
	    head: Head;
	    refs: Ref[];
	
	    static createFrom(source: any = {}) {
	        return new Refs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.head = this.convertValues(source["head"], Head);
	        this.refs = this.convertValues(source["refs"], Ref);
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

export namespace graph {
	
	export class Stash {
	    Hash: string;
	    Base: string;
	    Ref: string;
	    Subject: string;
	    Author: string;
	    Email: string;
	    Time: number;
	
	    static createFrom(source: any = {}) {
	        return new Stash(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Hash = source["Hash"];
	        this.Base = source["Base"];
	        this.Ref = source["Ref"];
	        this.Subject = source["Subject"];
	        this.Author = source["Author"];
	        this.Email = source["Email"];
	        this.Time = source["Time"];
	    }
	}

}

export namespace ops {
	
	export class CheckoutTarget {
	    kind: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new CheckoutTarget(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.name = source["name"];
	    }
	}
	export class CommitOptions {
	    message: string;
	    amend: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CommitOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	        this.amend = source["amend"];
	    }
	}
	export class Versions {
	    old: string;
	    new: string;
	    binary: boolean;
	    tooLarge: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Versions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.old = source["old"];
	        this.new = source["new"];
	        this.binary = source["binary"];
	        this.tooLarge = source["tooLarge"];
	    }
	}
	export class VersionsRequest {
	    path: string;
	    oldPath: string;
	    staged: boolean;
	    untracked: boolean;
	    commit: string;
	
	    static createFrom(source: any = {}) {
	        return new VersionsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.staged = source["staged"];
	        this.untracked = source["untracked"];
	        this.commit = source["commit"];
	    }
	}
	export class WipDiffRequest {
	    path: string;
	    staged: boolean;
	    untracked: boolean;
	    oldPath: string;
	
	    static createFrom(source: any = {}) {
	        return new WipDiffRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.staged = source["staged"];
	        this.untracked = source["untracked"];
	        this.oldPath = source["oldPath"];
	    }
	}

}

export namespace session {
	
	export class AutoFetchStatus {
	    enabled: boolean;
	    intervalSeconds: number;
	    paused: boolean;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new AutoFetchStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.intervalSeconds = source["intervalSeconds"];
	        this.paused = source["paused"];
	        this.reason = source["reason"];
	    }
	}
	export class FingerprintLine {
	    kind: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new FingerprintLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.text = source["text"];
	    }
	}
	export class HunkFingerprint {
	    oldStart: number;
	    oldLines: number;
	    newStart: number;
	    newLines: number;
	    lines: FingerprintLine[];
	
	    static createFrom(source: any = {}) {
	        return new HunkFingerprint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.oldStart = source["oldStart"];
	        this.oldLines = source["oldLines"];
	        this.newStart = source["newStart"];
	        this.newLines = source["newLines"];
	        this.lines = this.convertValues(source["lines"], FingerprintLine);
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
	export class StageLinesRequest {
	    path: string;
	    staged: boolean;
	    hunk: number;
	    lines: number[];
	    expect?: HunkFingerprint;
	
	    static createFrom(source: any = {}) {
	        return new StageLinesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.staged = source["staged"];
	        this.hunk = source["hunk"];
	        this.lines = source["lines"];
	        this.expect = this.convertValues(source["expect"], HunkFingerprint);
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

export namespace status {
	
	export class FileStatus {
	    path: string;
	    origPath: string;
	    index: string;
	    workTree: string;
	    conflicted: boolean;
	    untracked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.origPath = source["origPath"];
	        this.index = source["index"];
	        this.workTree = source["workTree"];
	        this.conflicted = source["conflicted"];
	        this.untracked = source["untracked"];
	    }
	}
	export class Status {
	    branch: string;
	    detached: boolean;
	    unborn: boolean;
	    upstream: string;
	    ahead: number;
	    behind: number;
	    files: FileStatus[];
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.detached = source["detached"];
	        this.unborn = source["unborn"];
	        this.upstream = source["upstream"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.files = this.convertValues(source["files"], FileStatus);
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

export namespace store {
	
	export class Draft {
	    summary: string;
	    body: string;
	
	    static createFrom(source: any = {}) {
	        return new Draft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = source["summary"];
	        this.body = source["body"];
	    }
	}
	export class Layout {
	    leftWidth: number;
	    rightWidth: number;
	
	    static createFrom(source: any = {}) {
	        return new Layout(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.leftWidth = source["leftWidth"];
	        this.rightWidth = source["rightWidth"];
	    }
	}
	export class Settings {
	    autoFetch: boolean;
	    autoFetchSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autoFetch = source["autoFetch"];
	        this.autoFetchSeconds = source["autoFetchSeconds"];
	    }
	}
	export class Tab {
	    id: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new Tab(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	    }
	}
	export class State {
	    tabs: Tab[];
	    activeTab: string;
	    recents: string[];
	    favorites: string[];
	    drafts: Record<string, Draft>;
	    layout: Layout;
	    settings: Settings;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tabs = this.convertValues(source["tabs"], Tab);
	        this.activeTab = source["activeTab"];
	        this.recents = source["recents"];
	        this.favorites = source["favorites"];
	        this.drafts = this.convertValues(source["drafts"], Draft, true);
	        this.layout = this.convertValues(source["layout"], Layout);
	        this.settings = this.convertValues(source["settings"], Settings);
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

