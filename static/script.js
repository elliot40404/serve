let ws;
let directoryData = {};
let currentSort = {
  column: "name",
  order: "asc",
};
let currentPath = "";

function getCurrentPath() {
  const path = window.location.pathname;
  if (path.startsWith("/browse/")) {
    return decodeURIComponent(path.substring(8));
  }
  return "";
}

function updateURL(path) {
  const urlPathSegments = path
    .split("/")
    .map((segment) => encodeURIComponent(segment))
    .join("/");
  const url = path ? `/browse/${urlPathSegments}` : "/";
  window.history.pushState({ path: path }, "", url);
}

function formatFileSize(bytes) {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}

function formatDate(dateString) {
  const date = new Date(dateString);
  return (
    date.toLocaleDateString() +
    " " +
    date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  );
}

function getFileIcon(file) {
  if (file.isDir) {
    return "📁";
  }
  const ext = file.name.split(".").pop().toLowerCase();
  const mediaExts = [
    "mp3",
    "wav",
    "flac",
    "aac",
    "ogg",
    "m4a",
    "wma",
    "mp4",
    "avi",
    "mkv",
    "mov",
    "wmv",
    "flv",
    "webm",
    "m4v",
    "jpg",
    "jpeg",
    "png",
    "gif",
    "webp",
    "svg",
    "bmp",
    "tiff",
  ];
  if (mediaExts.includes(ext)) {
    if (["mp3", "wav", "flac", "aac", "ogg", "m4a", "wma"].includes(ext))
      return "🎵";
    if (["mp4", "avi", "mkv", "mov", "wmv", "flv", "webm", "m4v"].includes(ext))
      return "🎬";
    return "🖼️";
  }
  return "📄";
}

function getFileClass(file) {
  if (file.isDir) return "directory";
  const ext = file.name.split(".").pop().toLowerCase();
  const mediaExts = [
    "mp3",
    "wav",
    "flac",
    "aac",
    "ogg",
    "m4a",
    "wma",
    "mp4",
    "avi",
    "mkv",
    "mov",
    "wmv",
    "flv",
    "webm",
    "m4v",
    "jpg",
    "jpeg",
    "png",
    "gif",
    "webp",
    "svg",
    "bmp",
    "tiff",
  ];
  if (mediaExts.includes(ext)) return "media";
  return "other";
}

function renderBreadcrumb(data) {
  const breadcrumb = document.getElementById("breadcrumb");
  const path = data.currentPath; // Clean, unencoded current path

  let html = `<a href="#" class="nav-link" data-path="">🏠 Home</a>`;

  if (path) {
    const parts = path.split("/");
    let currentPathBuild = "";
    for (let i = 0; i < parts.length; i++) {
      currentPathBuild += (i > 0 ? "/" : "") + parts[i];
      const isLast = i === parts.length - 1;
      html += '<span class="separator">›</span>';
      if (isLast) {
        html += `<span class="current">${parts[i]}</span>`;
      } else {
        html += `<a href="#" class="nav-link" data-path="${currentPathBuild}">${parts[i]}</a>`;
      }
    }
  }
  breadcrumb.innerHTML = html;
}

function renderFileList(data) {
  const fileList = document.getElementById("fileList");
  if (!data.files || data.files.length === 0) {
    fileList.innerHTML =
      '<div class="empty-state"><h3>Empty Directory</h3><p>This directory contains no files or folders.</p></div>';
    return;
  }

  let html = "";
  if (data.hasParent) {
    let logicalParentPath = ""; // Default to root (empty string path)
    if (data.currentPath) {
      const lastSlashIndex = data.currentPath.lastIndexOf("/");
      if (lastSlashIndex !== -1) {
        logicalParentPath = data.currentPath.substring(0, lastSlashIndex);
      } else {
        logicalParentPath = "";
      }
    }

    html += `
            <div class="file-item directory">
                <div class="file-name">
                    <span class="file-icon">📁</span>
                    <a href="#" class="nav-link" data-path="${logicalParentPath}">..</a>
                </div>
                <div class="file-size">-</div>
                <div class="file-date">-</div>
                <div class="file-permissions">-</div>
            </div>
        `;
  }

  for (const file of data.files) {
    const fileClass = getFileClass(file);
    const icon = getFileIcon(file);
    const size = file.isDir ? "-" : formatFileSize(file.size);
    const date = formatDate(file.modTime);
    let linkAttributes = "";

    if (file.isDir) {
      const newPath = data.currentPath
        ? `${data.currentPath}/${file.name}`
        : file.name;
      linkAttributes = `href="#" class="nav-link" data-path="${newPath}"`;
    } else {
      linkAttributes = `href="${file.path}" target="_blank"`;
    }

    html += `
            <div class="file-item ${fileClass}">
                <div class="file-name">
                    <span class="file-icon">${icon}</span>
                    <a ${linkAttributes}>${file.name}</a>
                </div>
                <div class="file-size">${size}</div>
                <div class="file-date">${date}</div>
                <div class="file-permissions">${file.mode}</div>
            </div>
        `;
  }
  fileList.innerHTML = html;
}

function updateSortIndicators() {
  document.querySelectorAll(".sort-indicator").forEach((indicator) => {
    indicator.textContent = "▲";
    indicator.classList.remove("desc");
  });
  const currentHeader = document.querySelector(
    `.file-list-header [data-sort="${currentSort.column}"]`,
  );
  if (currentHeader) {
    const indicator = currentHeader.querySelector(".sort-indicator");
    if (indicator) {
      if (currentSort.order === "desc") indicator.classList.add("desc");
    }
  }
}

function navigateToPath(path) {
  // path is expected to be the clean, unencoded relative file path
  currentPath = path;
  updateURL(path);
  loadDirectory(path);
}

function loadDirectory(path = "") {
  const params = new URLSearchParams({
    path: path,
    sort: currentSort.column,
    order: currentSort.order,
  });

  fetch(`/api/files?${params}`)
    .then((response) => {
      if (!response.ok)
        throw new Error(`HTTP error! status: ${response.status}`);
      return response.json();
    })
    .then((data) => {
      directoryData = data;
      renderBreadcrumb(data);
      renderFileList(data);
      updateSortIndicators();
    })
    .catch((error) => {
      console.error("Error loading directory:", error);
      const fileList = document.getElementById("fileList");
      fileList.innerHTML =
        '<div class="empty-state"><h3>Error</h3><p>Failed to load directory contents. Check console for details.</p></div>';
    });
}

function handleSort(column) {
  if (currentSort.column === column) {
    currentSort.order = currentSort.order === "asc" ? "desc" : "asc";
  } else {
    currentSort.column = column;
    currentSort.order = "asc";
  }
  document.getElementById("sortBy").value = currentSort.column;
  document.getElementById("sortOrder").value = currentSort.order;
  loadDirectory(currentPath);
}

function playRandomMedia() {
  const btn = document.getElementById("playRandomBtn");
  btn.disabled = true;
  btn.textContent = "🎲 Loading...";
  fetch(`/api/random-media?path=${encodeURIComponent(currentPath)}`)
    .then((response) => {
      if (!response.ok)
        return response.text().then((text) => {
          throw new Error(text || "No media files found or server error");
        });
      return response.text();
    })
    .then((mediaPath) => {
      if (mediaPath) window.open(mediaPath, "_blank");
      else throw new Error("Empty media path received");
    })
    .catch((error) => {
      console.error("Error playing random media:", error);
      alert(error.message || "No media files found or an error occurred.");
    })
    .finally(() => {
      btn.disabled = false;
      btn.textContent = "🎲 Play Random Media";
    });
}

function initWebSocket() {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const wsUrl = `${protocol}//${window.location.host}/ws`;
  if (
    ws &&
    (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)
  )
    return;
  updateConnectionStatus("connecting");
  ws = new WebSocket(wsUrl);
  ws.onopen = () => {
    console.log("WebSocket connected");
    updateConnectionStatus("connected");
    loadDirectory(currentPath);
  };
  ws.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      if (data.type === "update") {
        console.log("Update received, reloading...");
        loadDirectory(currentPath);
      }
    } catch (e) {
      console.error("Error processing WebSocket message:", e);
    }
  };
  ws.onclose = (event) => {
    console.log(
      "WebSocket disconnected. Code:",
      event.code,
      "Reason:",
      event.reason,
    );
    updateConnectionStatus("disconnected");
    setTimeout(initWebSocket, 3000);
  };
  ws.onerror = (error) => {
    console.error("WebSocket error:", error);
    updateConnectionStatus("disconnected");
  };
}

function updateConnectionStatus(status) {
  const statusBar = document.getElementById("statusBar");
  statusBar.className = `status-bar ${status}`;
}

function handleScroll() {
  const goToTopBtn = document.getElementById("goToTop");
  if (window.pageYOffset > 300) goToTopBtn.classList.add("visible");
  else goToTopBtn.classList.remove("visible");
}

function scrollToTop() {
  window.scrollTo({ top: 0, behavior: "smooth" });
}

document.addEventListener("DOMContentLoaded", function () {
  currentPath = getCurrentPath();
  loadDirectory(currentPath);
  initWebSocket();

  document.body.addEventListener("click", function (event) {
    const target = event.target.closest("a.nav-link");
    if (target && target.hasAttribute("data-path")) {
      event.preventDefault();
      const path = target.getAttribute("data-path");
      navigateToPath(path);
    }
  });

  document.getElementById("sortBy").addEventListener("change", function () {
    currentSort.column = this.value;
    loadDirectory(currentPath);
  });
  document.getElementById("sortOrder").addEventListener("change", function () {
    currentSort.order = this.value;
    loadDirectory(currentPath);
  });
  document
    .querySelectorAll(".file-list-header [data-sort]")
    .forEach((header) => {
      header.addEventListener("click", function () {
        handleSort(this.dataset.sort);
      });
    });
  document
    .getElementById("playRandomBtn")
    .addEventListener("click", playRandomMedia);
  document.getElementById("goToTop").addEventListener("click", scrollToTop);
  window.addEventListener("scroll", handleScroll);
  window.addEventListener("popstate", function (event) {
    currentPath =
      event.state && event.state.path !== undefined
        ? event.state.path
        : getCurrentPath();
    loadDirectory(currentPath);
  });
  if (!window.history.state || window.history.state.path === undefined) {
    window.history.replaceState(
      { path: currentPath },
      "",
      window.location.pathname,
    );
  }
});
