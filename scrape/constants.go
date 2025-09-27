package scrape

import (
	_ "embed"
	"time"
)

const (
	waitDefault       = time.Second
	cookieWaitDefault = 3 * time.Second
	maxRetries        = 3
)

//go:embed stealth.js
var stealthJS string

var waitDomains = []string{
	"reddit.com",
}

var cookieAddonDomains = []string{
	"yahoo.com",
}

var mediaExtensions = []string{
	"png", "jpg", "jpeg", "gif", "svg", "mp3", "mp4", "avi", "flac", "ogg", "wav", "webm", "webp",
}

var tagsToRemove = []string{
	"head",
	"script",
	"style",
	"noscript",
	"meta",
	"header",
	"footer",
	"nav",
	"aside",
	".header",
	".top",
	".navbar",
	"#header",
	".footer",
	".bottom",
	"#footer",
	".sidebar",
	".side",
	".aside",
	"#sidebar",
	".modal",
	".popup",
	"#modal",
	".overlay",
	".ad",
	".ads",
	".advert",
	"#ad",
	".lang-selector",
	".language",
	"#language-selector",
	".social",
	".social-media",
	".social-links",
	"#social",
	".menu",
	".navigation",
	"#nav",
	".breadcrumbs",
	"#breadcrumbs",
	".share",
	"#share",
	".widget",
	"#widget",
	".cookie",
	"#cookie",
}

var tagsToKeep = []string{
	"body",
	"main",
	"section",
	"article",
}

var addServingDomains = []string{
	"doubleclick.net",
	"adservice.google.com",
	"googlesyndication.com",
	"googletagservices.com",
	"googletagmanager.com",
	"google-analytics.com",
	"adsystem.com",
	"adservice.com",
	"adnxs.com",
	"ads-twitter.com",
	"facebook.net",
	"fbcdn.net",
	"amazon-adsystem.com",
}

var statusCodes = map[int]string{
	300: "Multiple Choices",
	301: "Moved Permanently",
	302: "Found",
	303: "See Other",
	304: "Not Modified",
	305: "Use Proxy",
	307: "Temporary Redirect",
	308: "Permanent Redirect",
	309: "Resume Incomplete",
	310: "Too Many Redirects",
	311: "Unavailable For Legal Reasons",
	312: "Previously Used",
	313: "I'm Used",
	314: "Switch Proxy",
	315: "Temporary Redirect",
	316: "Resume Incomplete",
	317: "Too Many Redirects",
	400: "Bad Request",
	401: "Unauthorized",
	403: "Forbidden",
	404: "Not Found",
	405: "Method Not Allowed",
	406: "Not Acceptable",
	407: "Proxy Authentication Required",
	408: "Request Timeout",
	409: "Conflict",
	410: "Gone",
	411: "Length Required",
	412: "Precondition Failed",
	413: "Payload Too Large",
	414: "URI Too Long",
	415: "Unsupported Media Type",
	416: "Range Not Satisfiable",
	417: "Expectation Failed",
	418: "I'm a teapot",
	421: "Misdirected Request",
	422: "Unprocessable Entity",
	423: "Locked",
	424: "Failed Dependency",
	425: "Too Early",
	426: "Upgrade Required",
	428: "Precondition Required",
	429: "Too Many Requests",
	431: "Request Header Fields Too Large",
	451: "Unavailable For Legal Reasons",
	500: "Internal Server Error",
	501: "Not Implemented",
	502: "Bad Gateway",
	503: "Service Unavailable",
	504: "Gateway Timeout",
	505: "HTTP Version Not Supported",
	506: "Variant Also Negotiates",
	507: "Insufficient Storage",
	508: "Loop Detected",
	510: "Not Extended",
	511: "Network Authentication Required",
	599: "Network Connect Timeout Error",
}
