export namespace core {
	
	export class BackupInfo {
	    error?: string;
	    version: string;
	    exported: string;
	    servers: number;
	    shortcuts: number;
	    themes: number;
	    hasPasswords: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BackupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.error = source["error"];
	        this.version = source["version"];
	        this.exported = source["exported"];
	        this.servers = source["servers"];
	        this.shortcuts = source["shortcuts"];
	        this.themes = source["themes"];
	        this.hasPasswords = source["hasPasswords"];
	    }
	}
	export class BackupOpen {
	    data: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new BackupOpen(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.data = source["data"];
	        this.error = source["error"];
	    }
	}
	export class BackupResult {
	    data: string;
	    name: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new BackupResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.data = source["data"];
	        this.name = source["name"];
	        this.error = source["error"];
	    }
	}
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
	export class ClientCert {
	    cert: string;
	    key: string;
	    fingerprint: string;
	    sha512: string;
	    subject: string;
	    expires: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ClientCert(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cert = source["cert"];
	        this.key = source["key"];
	        this.fingerprint = source["fingerprint"];
	        this.sha512 = source["sha512"];
	        this.subject = source["subject"];
	        this.expires = source["expires"];
	        this.error = source["error"];
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
	export class MusicSettings {
	    enabled: boolean;
	    trigger: string;
	    source: string;
	    file: string;
	    format: string;
	    npFormat: string;
	    onlyMe: boolean;
	    cooldown: number;
	
	    static createFrom(source: any = {}) {
	        return new MusicSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.trigger = source["trigger"];
	        this.source = source["source"];
	        this.file = source["file"];
	        this.format = source["format"];
	        this.npFormat = source["npFormat"];
	        this.onlyMe = source["onlyMe"];
	        this.cooldown = source["cooldown"];
	    }
	}
	export class PluginSettings {
	    music: MusicSettings;
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
	        this.music = this.convertValues(source["music"], MusicSettings);
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
	export class Favourite {
	    server: string;
	    buffer: string;
	
	    static createFrom(source: any = {}) {
	        return new Favourite(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = source["server"];
	        this.buffer = source["buffer"];
	    }
	}
	export class NotifyRule {
	    level: string;
	    until?: number;
	
	    static createFrom(source: any = {}) {
	        return new NotifyRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.until = source["until"];
	    }
	}
	export class Settings {
	    theme: string;
	    lightTheme: string;
	    darkTheme: string;
	    highlightWords: string[];
	    notifyList: string[];
	    notifyFriends: boolean;
	    sendTyping: boolean;
	    notifications: boolean;
	    notifyPMs: boolean;
	    inlineImages: boolean;
	    inlineYouTube: boolean;
	    inlineVideos: boolean;
	    showJoinsParts: boolean;
	    timeFormat: string;
	    showSeconds: boolean;
	    fontSize: number;
	    fontFamily: string;
	    logging: boolean;
	    historyLines: number;
	    textLogs: boolean;
	    logFolder: string;
	    logMaxLines: number;
	    showUserList: boolean;
	    compactMode: boolean;
	    chatLayout: string;
	    quitMessage: string;
	    startWithWindows: boolean;
	    startMinimized: boolean;
	    confirmOnClose: boolean;
	    markerLine: boolean;
	    nickContrast: boolean;
	    ignore: string[];
	    language: string;
	    filterWords: string[];
	    autoUpdateCheck: boolean;
	    skippedVersion: string;
	    trayIcon: boolean;
	    flashOnPing: boolean;
	    closeToTray: boolean;
	    pasteWarnLines: number;
	    autoAway: boolean;
	    autoAwayMinutes: number;
	    autoAwayMessage: string;
	    soundHighlight: string;
	    soundPM: string;
	    soundVolume: number;
	    uploadHost: string;
	    imgurClientId: string;
	    uploadURL: string;
	    keepAlive: boolean;
	    linkPreviews: boolean;
	    spellCheck: boolean;
	    mentionPopup: boolean;
	    confirmModeration: boolean;
	    kickReason: string;
	    dccFolder: string;
	    dccAddress: string;
	    dccPorts: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.lightTheme = source["lightTheme"];
	        this.darkTheme = source["darkTheme"];
	        this.highlightWords = source["highlightWords"];
	        this.notifyList = source["notifyList"];
	        this.notifyFriends = source["notifyFriends"];
	        this.sendTyping = source["sendTyping"];
	        this.notifications = source["notifications"];
	        this.notifyPMs = source["notifyPMs"];
	        this.inlineImages = source["inlineImages"];
	        this.inlineYouTube = source["inlineYouTube"];
	        this.inlineVideos = source["inlineVideos"];
	        this.showJoinsParts = source["showJoinsParts"];
	        this.timeFormat = source["timeFormat"];
	        this.showSeconds = source["showSeconds"];
	        this.fontSize = source["fontSize"];
	        this.fontFamily = source["fontFamily"];
	        this.logging = source["logging"];
	        this.historyLines = source["historyLines"];
	        this.textLogs = source["textLogs"];
	        this.logFolder = source["logFolder"];
	        this.logMaxLines = source["logMaxLines"];
	        this.showUserList = source["showUserList"];
	        this.compactMode = source["compactMode"];
	        this.chatLayout = source["chatLayout"];
	        this.quitMessage = source["quitMessage"];
	        this.startWithWindows = source["startWithWindows"];
	        this.startMinimized = source["startMinimized"];
	        this.confirmOnClose = source["confirmOnClose"];
	        this.markerLine = source["markerLine"];
	        this.nickContrast = source["nickContrast"];
	        this.ignore = source["ignore"];
	        this.language = source["language"];
	        this.filterWords = source["filterWords"];
	        this.autoUpdateCheck = source["autoUpdateCheck"];
	        this.skippedVersion = source["skippedVersion"];
	        this.trayIcon = source["trayIcon"];
	        this.flashOnPing = source["flashOnPing"];
	        this.closeToTray = source["closeToTray"];
	        this.pasteWarnLines = source["pasteWarnLines"];
	        this.autoAway = source["autoAway"];
	        this.autoAwayMinutes = source["autoAwayMinutes"];
	        this.autoAwayMessage = source["autoAwayMessage"];
	        this.soundHighlight = source["soundHighlight"];
	        this.soundPM = source["soundPM"];
	        this.soundVolume = source["soundVolume"];
	        this.uploadHost = source["uploadHost"];
	        this.imgurClientId = source["imgurClientId"];
	        this.uploadURL = source["uploadURL"];
	        this.keepAlive = source["keepAlive"];
	        this.linkPreviews = source["linkPreviews"];
	        this.spellCheck = source["spellCheck"];
	        this.mentionPopup = source["mentionPopup"];
	        this.confirmModeration = source["confirmModeration"];
	        this.kickReason = source["kickReason"];
	        this.dccFolder = source["dccFolder"];
	        this.dccAddress = source["dccAddress"];
	        this.dccPorts = source["dccPorts"];
	    }
	}
	export class Shortcut {
	    trigger: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new Shortcut(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trigger = source["trigger"];
	        this.text = source["text"];
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
	    encoding: string;
	    channelOrder: string[];
	    bouncer: string;
	    bouncerUser: string;
	    bouncerPass: string;
	    bouncerNetwork: string;
	    encryptionKey?: string;
	    clientCert?: string;
	    clientKey?: string;
	    secretsStored?: boolean;
	    iconText?: string;
	    letterIcon?: boolean;
	    joinsParts?: string;
	    proxyType?: string;
	    proxyHost?: string;
	    proxyPort?: number;
	    proxyUser?: string;
	    proxyPass?: string;
	
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
	        this.encoding = source["encoding"];
	        this.channelOrder = source["channelOrder"];
	        this.bouncer = source["bouncer"];
	        this.bouncerUser = source["bouncerUser"];
	        this.bouncerPass = source["bouncerPass"];
	        this.bouncerNetwork = source["bouncerNetwork"];
	        this.encryptionKey = source["encryptionKey"];
	        this.clientCert = source["clientCert"];
	        this.clientKey = source["clientKey"];
	        this.secretsStored = source["secretsStored"];
	        this.iconText = source["iconText"];
	        this.letterIcon = source["letterIcon"];
	        this.joinsParts = source["joinsParts"];
	        this.proxyType = source["proxyType"];
	        this.proxyHost = source["proxyHost"];
	        this.proxyPort = source["proxyPort"];
	        this.proxyUser = source["proxyUser"];
	        this.proxyPass = source["proxyPass"];
	    }
	}
	export class StartBuffer {
	    server: string;
	    buffer: string;
	
	    static createFrom(source: any = {}) {
	        return new StartBuffer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = source["server"];
	        this.buffer = source["buffer"];
	    }
	}
	export class WindowState {
	    x: number;
	    y: number;
	    width: number;
	    height: number;
	    maximized: boolean;
	    fullscreen: boolean;
	    saved: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WindowState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.maximized = source["maximized"];
	        this.fullscreen = source["fullscreen"];
	        this.saved = source["saved"];
	    }
	}
	export class Config {
	    window: WindowState;
	    startView: StartBuffer;
	    servers: ServerConfig[];
	    shortcuts: Shortcut[];
	    settings: Settings;
	    nickColors: Record<string, string>;
	    channelNotify?: Record<string, NotifyRule>;
	    channelJoins?: Record<string, string>;
	    favourites?: Favourite[];
	    plugins: PluginSettings;
	    installId: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.window = this.convertValues(source["window"], WindowState);
	        this.startView = this.convertValues(source["startView"], StartBuffer);
	        this.servers = this.convertValues(source["servers"], ServerConfig);
	        this.shortcuts = this.convertValues(source["shortcuts"], Shortcut);
	        this.settings = this.convertValues(source["settings"], Settings);
	        this.nickColors = source["nickColors"];
	        this.channelNotify = this.convertValues(source["channelNotify"], NotifyRule, true);
	        this.channelJoins = source["channelJoins"];
	        this.favourites = this.convertValues(source["favourites"], Favourite);
	        this.plugins = this.convertValues(source["plugins"], PluginSettings);
	        this.installId = source["installId"];
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
	export class DCCTransfer {
	    id: string;
	    server: string;
	    nick: string;
	    kind: string;
	    dir: string;
	    file?: string;
	    size?: number;
	    done?: number;
	    state: string;
	    error?: string;
	    path?: string;
	    addr?: string;
	    risky?: boolean;
	    started?: number;
	    time: number;
	
	    static createFrom(source: any = {}) {
	        return new DCCTransfer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.server = source["server"];
	        this.nick = source["nick"];
	        this.kind = source["kind"];
	        this.dir = source["dir"];
	        this.file = source["file"];
	        this.size = source["size"];
	        this.done = source["done"];
	        this.state = source["state"];
	        this.error = source["error"];
	        this.path = source["path"];
	        this.addr = source["addr"];
	        this.risky = source["risky"];
	        this.started = source["started"];
	        this.time = source["time"];
	    }
	}
	export class Diagnostics {
	    version: string;
	    os: string;
	    logDir: string;
	    lastCrash: string;
	    crashText: string;
	    reported: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Diagnostics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.os = source["os"];
	        this.logDir = source["logDir"];
	        this.lastCrash = source["lastCrash"];
	        this.crashText = source["crashText"];
	        this.reported = source["reported"];
	    }
	}
	export class EncryptionKeyPair {
	    privateKey: string;
	    publicKey: string;
	
	    static createFrom(source: any = {}) {
	        return new EncryptionKeyPair(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.privateKey = source["privateKey"];
	        this.publicKey = source["publicKey"];
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
	
	export class LinkPreview {
	    url: string;
	    title: string;
	    description?: string;
	    image?: string;
	    site?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new LinkPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.image = source["image"];
	        this.site = source["site"];
	        this.error = source["error"];
	    }
	}
	export class MenuContext {
	    server: string;
	    buffer: string;
	    kind: string;
	    nick: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new MenuContext(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = source["server"];
	        this.buffer = source["buffer"];
	        this.kind = source["kind"];
	        this.nick = source["nick"];
	        this.text = source["text"];
	    }
	}
	export class MenuItem {
	    id?: string;
	    label?: string;
	    sep?: boolean;
	    items?: MenuItem[];
	
	    static createFrom(source: any = {}) {
	        return new MenuItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.sep = source["sep"];
	        this.items = this.convertValues(source["items"], MenuItem);
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
	
	
	export class PasteResult {
	    url: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new PasteResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.error = source["error"];
	    }
	}
	
	export class ReportInput {
	    server: string;
	    buffer: string;
	    nick: string;
	    reason: string;
	    note: string;
	    message: string;
	    context: string[];
	
	    static createFrom(source: any = {}) {
	        return new ReportInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = source["server"];
	        this.buffer = source["buffer"];
	        this.nick = source["nick"];
	        this.reason = source["reason"];
	        this.note = source["note"];
	        this.message = source["message"];
	        this.context = source["context"];
	    }
	}
	export class SearchHit {
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
	    serverName: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchHit(source);
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
	        this.serverName = source["serverName"];
	    }
	}
	export class SearchResult {
	    hits: SearchHit[];
	    more: boolean;
	    scanned: number;
	
	    static createFrom(source: any = {}) {
	        return new SearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hits = this.convertValues(source["hits"], SearchHit);
	        this.more = source["more"];
	        this.scanned = source["scanned"];
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
	
	export class UpdateInfo {
	    available: boolean;
	    current: string;
	    latest: string;
	    notes: string;
	    url: string;
	    size: number;
	    manual: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.notes = source["notes"];
	        this.url = source["url"];
	        this.size = source["size"];
	        this.manual = source["manual"];
	        this.error = source["error"];
	    }
	}
	export class UploadResult {
	    url: string;
	    expires: string;
	    host: string;
	
	    static createFrom(source: any = {}) {
	        return new UploadResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.expires = source["expires"];
	        this.host = source["host"];
	    }
	}
	

}

