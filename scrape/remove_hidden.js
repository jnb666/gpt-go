const root = document.body;
var found = true;
while (found) {
    found = false;
    for (const el of root.getElementsByTagName("*")) {
        if (el.offsetWidth === 0 && el.offsetHeight === 0) {
            el.remove();
            found = true;
            break;
        }
    }
}
root.getHTML();