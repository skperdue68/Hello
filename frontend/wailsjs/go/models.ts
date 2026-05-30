export namespace main {
	
	export class WatchEntry {
	    id: number;
	    path: string;
	    dir: string;
	    enabled: boolean;
	    lastHash: string;
	
	    static createFrom(source: any = {}) {
	        return new WatchEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.dir = source["dir"];
	        this.enabled = source["enabled"];
	        this.lastHash = source["lastHash"];
	    }
	}

}

