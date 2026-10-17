export namespace core {
	
	export class User {
	    nick: string;
	    prefix: string;
	    away: boolean;
	
	    static createFrom(source: any = {}) {
	        return new User(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nick = source["nick"];
	        this.prefix = source["prefix"];
	        this.away = source["away"];
	    }
	}
	export class ChannelState {
	    name: string;
	    topic: string;
	    modes: string;
	    users: User[];
	
	    static createFrom(source: any = {}) {
	        return new ChannelState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.topic = source["topic"];
	        this.modes = source["modes"];
	        this.users = this.convertValues(source["users"], User);
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
	export class URLCommand {
	    trigger: string;
	    url: string;
	    template: string;
	    maxLines: number;
	    onlyMe: boolean;
	    cooldown: number;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new URLCommand(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trigger = source["trigger"];
	        this.url = source["url"];
	        this.template = source["template"];
	        this.maxLines = source["maxLines"];
	        this.onlyMe = source["onlyMe"];
	        this.cooldown = source["cooldown"];
	        this.enabled = source["enabled"];
	    }
	}
	export class PluginSettings {
	    botPrefix: string;
	    weatherEnabled: boolean;
	    weatherChannels: string[];
	    weatherCooldown: number;
	    weatherFromMe: boolean;
	    weatherFromOthers: boolean;
	    urlCommands: URLCommand[];
	    scriptsEnabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.botPrefix = source["botPrefix"];
	        this.weatherEnabled = source["weatherEnabled"];
	        this.weatherChannels = source["weatherChannels"];
	        this.weatherCooldown = source["weatherCooldown"];
	        this.weatherFromMe = source["weatherFromMe"];
	        this.weatherFromOthers = source["weatherFromOthers"];
	        this.urlCommands = this.convertValues(source["urlCommands"], URLCommand);
	        this.scriptsEnabled = source["scriptsEnabled"];
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
	    theme: string;
	    lightTheme: string;
	    darkTheme: string;
	    highlightWords: string[];
	    notifications: boolean;
	    notifyPMs: boolean;
	    inlineImages: boolean;
	    inlineYouTube: boolean;
	    showJoinsParts: boolean;
	    timeFormat: string;
	    showSeconds: boolean;
	    fontSize: number;
	    fontFamily: string;
	    logging: boolean;
	    historyLines: number;
	    showUserList: boolean;
	    compactMode: boolean;
	    quitMessage: string;
	    ignore: string[];
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.lightTheme = source["lightTheme"];
	        this.darkTheme = source["darkTheme"];
	        this.highlightWords = source["highlightWords"];
	        this.notifications = source["notifications"];
	        this.notifyPMs = source["notifyPMs"];
	        this.inlineImages = source["inlineImages"];
	        this.inlineYouTube = source["inlineYouTube"];
	        this.showJoinsParts = source["showJoinsParts"];
	        this.timeFormat = source["timeFormat"];
	        this.showSeconds = source["showSeconds"];
	        this.fontSize = source["fontSize"];
	        this.fontFamily = source["fontFamily"];
	        this.logging = source["logging"];
	        this.historyLines = source["historyLines"];
	        this.showUserList = source["showUserList"];
	        this.compactMode = source["compactMode"];
	        this.quitMessage = source["quitMessage"];
	        this.ignore = source["ignore"];
	    }
	}
	export class ServerConfig {
	    id: string;
	    name: string;
	    host: string;
	    port: number;
	    tls: boolean;
	    tlsSkipVerify: boolean;
	    password: string;
	    nick: string;
	    altNick: string;
	    username: string;
	    realname: string;
	    authMethod: string;
	    authUser: string;
	    authPass: string;
	    autoConnect: boolean;
	    autoJoin: string[];
	    rememberChannels: boolean;
	    perform: string[];
	
	    static createFrom(source: any = {}) {
	        return new ServerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.tls = source["tls"];
	        this.tlsSkipVerify = source["tlsSkipVerify"];
	        this.password = source["password"];
	        this.nick = source["nick"];
	        this.altNick = source["altNick"];
	        this.username = source["username"];
	        this.realname = source["realname"];
	        this.authMethod = source["authMethod"];
	        this.authUser = source["authUser"];
	        this.authPass = source["authPass"];
	        this.autoConnect = source["autoConnect"];
	        this.autoJoin = source["autoJoin"];
	        this.rememberChannels = source["rememberChannels"];
	        this.perform = source["perform"];
	    }
	}
	export class Config {
	    servers: ServerConfig[];
	    settings: Settings;
	    nickColors: Record<string, string>;
	    plugins: PluginSettings;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.servers = this.convertValues(source["servers"], ServerConfig);
	        this.settings = this.convertValues(source["settings"], Settings);
	        this.nickColors = source["nickColors"];
	        this.plugins = this.convertValues(source["plugins"], PluginSettings);
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
	export class Event {
	    type: string;
	    server: string;
	    buffer: string;
	    kind?: string;
	    nick?: string;
	    prefix?: string;
	    text?: string;
	    time: number;
	    self?: boolean;
	    highlight?: boolean;
	    extra?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.server = source["server"];
	        this.buffer = source["buffer"];
	        this.kind = source["kind"];
	        this.nick = source["nick"];
	        this.prefix = source["prefix"];
	        this.text = source["text"];
	        this.time = source["time"];
	        this.self = source["self"];
	        this.highlight = source["highlight"];
	        this.extra = source["extra"];
	    }
	}
	
	
	export class ServerState {
	    id: string;
	    status: string;
	    nick: string;
	    network: string;
	    channels: ChannelState[];
	
	    static createFrom(source: any = {}) {
	        return new ServerState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.status = source["status"];
	        this.nick = source["nick"];
	        this.network = source["network"];
	        this.channels = this.convertValues(source["channels"], ChannelState);
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
	
	export class Theme {
	    name: string;
	    base: string;
	    vars: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Theme(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.base = source["base"];
	        this.vars = source["vars"];
	    }
	}
	

}

