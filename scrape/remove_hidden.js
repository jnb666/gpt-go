
function removeHidden(root) {
	for (const el of root.getElementsByTagName("*")) {
	    const style = window.getComputedStyle(el);
    	if ((style.display === "none") || (style.visibility === "hidden")) {
    		const name = el.tagName;
    		el.remove();
    		return true;
    	}
	}
	return false;
}

function removeHiddenElements() {
	const root = document.body;
	while (removeHidden(root)) {}
	return root.getHTML()
}
