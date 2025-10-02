
function newElement(tag, classes, child) {
	const elem = document.createElement(tag);
	if (classes) {
		elem.setAttribute("class", classes);
	}
	if (child) {
		elem.appendChild(child);
	}
	return elem;
}

function scrollToEnd() {
	window.scrollTo(0, document.body.scrollHeight);
}

function selectLink(list, id) {
	showConfigForm(false);
	for (const el of list.querySelectorAll("a")) {
		const item = el.getAttribute("id");
		el.setAttribute("class", "pure-menu-link" + ((item === id) ? " selected-link" : ""));
	}
}

function refreshChatList(model, list, currentID) {
	console.log("refresh chat list: current=%s", currentID);
	const parent = document.getElementById("conv-list");
	parent.replaceChildren();
	if (list) {
		for (const item of list) {
			const link = newElement("a", "pure-menu-link");
			link.setAttribute("id", item.id);
			link.textContent = item.summary;
			const entry = newElement("li", "pure-menu-item", link);
			parent.appendChild(entry);
		}
		if (currentID) {
			selectLink(parent, currentID);
		}
	}
}

function addMessage(chat, type, content, update, hidden) {
	if (!update) {
		const entry = newElement("li", "chat-item " + type, newElement("div", "msg"));
		if (hidden) {
			entry.style.display = "none";
		}
		chat.appendChild(entry);
	}
	const nodes = chat.querySelectorAll("div.msg");
	if (nodes.length == 0) {
		console.error("chat update error: empty node list");
		return;
	}
	const element = nodes[nodes.length - 1];
	element.innerHTML = content;
	scrollToEnd();
}

function loadChat(chat, conv, showReasoning) {
	console.log("load chat %s reasoning=%s", conv.id, showReasoning);
	chat.replaceChildren();
	if (conv.messages) {
		for (const msg of conv.messages) {
			addMessage(chat, msg.type, msg.content, false, msg.type == "analysis" && !showReasoning);
		}
	}
}

function refreshChat(chat, showReasoning) {
	console.log("refresh chat reasoning=%s", showReasoning);
	for (const item of chat.querySelectorAll("li.analysis")) {
		item.style.display = (showReasoning) ? "flex" : "none";
	}
}

function showConfigForm(on) {
	document.getElementById("chat-list").style.display = (on) ? "none" : "block";
	document.getElementById("config-page").style.display = (on) ? "block" : "none";	
}

function showConfig(cfg) {
	console.log("show config", cfg);
	showConfigForm(true);
	const form = document.getElementById("config-form");
	const radio = form.querySelectorAll(`input[name="reasoning"]`);
	
	form.system.value = cfg.system_prompt;
	for (const el of radio) {
		el.checked = (el.value == cfg.reasoning_effort);
	}
	const parent = document.getElementById("tools-list");
	parent.replaceChildren();
	if (cfg.tools) {
		for (const tool of cfg.tools) {
			const checkbox = newElement("div", "tool-checkbox");
			const checked = (tool.enabled) ? "checked" : "";
			checkbox.innerHTML = `<input id="${tool.name}-tool" name="${tool.name}_tool" type="checkbox" ${checked}> <label for="${tool.name}-tool">${tool.name}</label><br>`;
			parent.appendChild(checkbox);
		}
	}
}

function duration(ms) {
	return (ms >= 1000) ? (ms/1000).toFixed(1)+"s" : ms+"ms";
}

function clearStats() {
	for (const key of ["stats-calls", "stats-tools", "stats-tokens", "stats-speed"]) {
		document.getElementById(key).textContent = "";
	}
}

function updateStats(stats) {
	const tokensPerSec = 1000 * stats.completion_tokens / stats.api_time;
	document.getElementById("model-name").textContent = stats.model;
	document.getElementById("stats-calls").textContent = `${stats.api_calls} API calls in ${duration(stats.api_time)}`;
	if (stats.tool_calls) {
		document.getElementById("stats-tools").textContent = `${stats.tool_calls} tool calls in ${duration(stats.tool_time)}`;
	} else {
		document.getElementById("stats-tools").textContent = "";
	}
	document.getElementById("stats-tokens").textContent = `${stats.prompt_tokens}+${stats.completion_tokens} tokens`;
	document.getElementById("stats-speed").textContent = `${tokensPerSec.toFixed(1)} tok/sec`;
}

function initFormControls(app) {
	const form = document.getElementById("config-form");

	form.addEventListener("submit", e => {
		e.preventDefault();
		const cfg = {
			system_prompt: form.system.value,
			reasoning_effort: "medium",
			tools: []
		};
		const radio = form.querySelectorAll(`input[name="reasoning"]`);
		for (const el of radio) {
			if (el.checked) cfg.reasoning_effort = el.value;
		}
		const tools = form.querySelectorAll(`.tool-checkbox input`);
		for (const el of tools) {
			cfg.tools.push({ name: el.name.slice(0, -5), enabled: el.checked });	
		}
		console.log("update config", cfg);
		app.send({ action: "config", config: cfg });
	})
}

function initMenuControls(app) {
	const list = document.getElementById("conv-list");

	list.addEventListener("click", e => {
		const link = e.target.closest("a");
		if (link) {
			const id = link.getAttribute("id");
			selectLink(list, id);
			app.send({ action: "load", id: id });
		}
	});

	document.getElementById("new-chat").addEventListener("click", e => {
		selectLink(list, "");
		app.send({ action: "load" });
	});

	document.getElementById("del-chat").addEventListener("click", e => {
		showConfigForm(false);
		for (const el of list.querySelectorAll("a")) {
			if (el.getAttribute("class").includes("selected-link")) {
				app.send({ action: "delete", id: el.getAttribute("id") });
				break;
			}
		}
	});

	document.getElementById("options").addEventListener("click", e => {
		app.send({ action: "config" });
	});

	const checkbox = document.getElementById("reasoning-history");
	checkbox.addEventListener("click", e => {
		app.showReasoning = checkbox.checked;
		refreshChat(app.chat, app.showReasoning);
	});
}

function initChatControls(app) {
	app.chat.addEventListener("click", e => {
		const collapsed = e.target.closest(".tool-response");
		if (collapsed) {
			collapsed.setAttribute("class", "tool-response-expanded");
			return;
		}
		const expanded = e.target.closest(".tool-response-expanded");
		if (expanded) {
			expanded.setAttribute("class", "tool-response");
		}
	});
}

function initInputTextbox(app) {
	const input = document.getElementById("input-text");

	const submit = function () {
		console.log("send add message");
		showConfigForm(false);
		const msg = input.value;
		if (msg.trim() == "") {
			input.placeholder = "Please enter a question";
			input.value = "";
			return;
		}
		input.value = "";
		input.setAttribute("class", "input-default");

		if (!app.showReasoning) {
			refreshChat(app.chat, false);
		}
		addMessage(app.chat, "user", `<p>${msg}</p>`);
		clearStats();
		app.send({ action: "add", message: { type: "user", content: msg } });
		input.placeholder = "Type a message (Shift+Enter to add a new line)";
	}

	input.addEventListener("keypress", e => {
		if (e.key === "Enter") {
			if (e.shiftKey) {
				input.setAttribute("class", "input-expanded");
			} else {
				e.preventDefault();
				submit();
			}
		}
	});
	input.addEventListener("blur", e => {
		input.setAttribute("class", "input-default");
	});
	document.getElementById("send-button").addEventListener("click", submit);
}

// Websocket communication with server
class App {
	connected = false;

	constructor() {
		this.socket = this.initWebsocket();
		this.chat = document.getElementById("chat-list");
		this.showReasoning = document.getElementById("reasoning-history").checked;
		initInputTextbox(this);
		initMenuControls(this);
		initFormControls(this);
		initChatControls(this);
	}

	initWebsocket() {
		const socket = new WebSocket("/websocket");
		socket.addEventListener("open", e => {
			console.log("websocket connected");
			this.connected = true;
			this.send({ action: "list" });
		});
		socket.addEventListener("close", e => {
			console.log("websocket closed");
			this.connected = false;
		});
		socket.addEventListener("message", e => {
			const resp = JSON.parse(e.data);
			this.recv(resp);
		});
		return socket;		
	}

	recv(resp) {
		switch (resp.action) {
			case "add":
				addMessage(this.chat, resp.message.type, resp.message.content, resp.message.update);
				if (resp.message.end && !this.showReasoning) {
					refreshChat(app.chat, false);
				}
				break;
			case "stats":
				updateStats(resp.stats);
				break;
			case "list":
				const id = (resp.conversation) ? resp.conversation.id : "";
				refreshChatList(resp.model, resp.list, id);
				break;
			case "load":
				loadChat(this.chat, resp.conversation, this.showReasoning);
				break
			case "config":
				showConfig(resp.config);
				break
			default:
				console.error("unknown action", resp.action)
		}
	}

	send(req) {
		if (!this.connected) {
			document.getElementById("input-text").placeholder = "Error: websocket not connected";
			return;
		}
		this.socket.send(JSON.stringify(req));
	}
}

const app = new App();


