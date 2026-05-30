export namespace main {
	
	export class WatchEntry {
	    id: number;
	    path: string;
	    dir: string;
	    fileName: string;
	    enabled: boolean;
	    lastHash: string;
	    remoteHash: string;
	    lastUploadedBy: string;
	    lastUploadedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new WatchEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.dir = source["dir"];
	        this.fileName = source["fileName"];
	        this.enabled = source["enabled"];
	        this.lastHash = source["lastHash"];
	        this.remoteHash = source["remoteHash"];
	        this.lastUploadedBy = source["lastUploadedBy"];
	        this.lastUploadedAt = source["lastUploadedAt"];
	    }
	}

}

