package vfs

import (
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

var (
	configExtensions = map[string]struct{}{
		"toml": {}, "yaml": {}, "yml": {}, "json": {}, "jsonc": {}, "ini": {}, "conf": {},
		"config": {}, "env": {}, "properties": {}, "xml": {}, "mod": {}, "sum": {}, "lock": {},
	}
	textExtensions = map[string]struct{}{
		"txt": {}, "md": {}, "rst": {}, "go": {}, "rs": {}, "c": {}, "h": {}, "cpp": {}, "hpp": {},
		"py": {}, "js": {}, "ts": {}, "tsx": {}, "jsx": {}, "java": {}, "kt": {}, "kts": {}, "swift": {},
		"html": {}, "css": {}, "scss": {}, "sass": {}, "less": {}, "styl": {},
		"sh": {}, "bash": {}, "zsh": {}, "fish": {}, "sql": {},
		"log": {}, "csv": {}, "tsv": {},
		"lua": {}, "rb": {}, "pl": {}, "pm": {}, "t": {}, "ps1": {}, "bat": {}, "cmd": {},
		"vue": {}, "svelte": {}, "astro": {}, "ejs": {}, "hbs": {}, "pug": {}, "haml": {}, "php": {}, "twig": {},
		"scala": {}, "groovy": {}, "clj": {}, "ex": {}, "exs": {}, "elm": {}, "hs": {}, "lisp": {}, "cl": {}, "rkt": {}, "scm": {}, "dart": {},
		"tex": {}, "bib": {}, "sty": {}, "cls": {},
		"gradle": {}, "cmake": {}, "mk": {}, "mak": {},
		"asm": {}, "s": {}, "inc": {},
		"patch": {}, "diff": {},
		"proto": {}, "graphql": {}, "gql": {},
		"tf": {}, "hcl": {},
		"r": {}, "m": {}, "mm": {},
		"nim": {}, "zig": {}, "odin": {}, "v": {},
		"cr": {}, "jl": {},
		"erl": {}, "hrl": {},
	}
	// textFilenames lists common text files without a meaningful extension
	// (like Makefile, Dockerfile, etc.) so they open in the editor.
	textFilenames = map[string]struct{}{
		"makefile": {}, "dockerfile": {}, "containerfile": {},
		"readme": {}, "license": {}, "licence": {}, "copying": {}, "changelog": {}, "changes": {},
		"todo": {}, "notes": {}, "authors": {}, "contributors": {}, "maintainers": {},
		"procfile": {}, "gemfile": {}, "rakefile": {}, "snapfile": {}, "fastfile": {},
		"cmakelists": {}, "justfile": {}, "taskfile": {},
		"gitignore": {}, "gitattributes": {}, "gitmodules": {}, "gitkeep": {},
		"gitconfig": {}, "git-blame-ignore-revs": {},
		"editorconfig": {}, "envrc": {}, "hushlogin": {},
		"xsession": {}, "xresources": {}, "xinitrc": {},
		"bashrc": {}, "bash_profile": {}, "bash_logout": {},
		"zshrc": {}, "zprofile": {}, "zlogin": {}, "zlogout": {},
		"profile": {}, "inputrc": {}, "tmux.conf": {},
		"npmrc": {}, "yarnrc": {}, "pnpmrc": {},
		"eslintrc": {}, "prettierrc": {}, "babelrc": {},
		"stylelintrc": {}, "commitlintrc": {},
		"htaccess": {}, "htpasswd": {},
	}
	imageExtensions = map[string]struct{}{
		"png": {}, "jpg": {}, "jpeg": {}, "gif": {}, "webp": {}, "bmp": {}, "svg": {}, "ico": {},
		"avif": {}, "heic": {}, "heif": {}, "tiff": {}, "tif": {},
	}
	archiveExtensions = map[string]struct{}{
		"zip": {}, "tar": {}, "gz": {}, "tgz": {}, "xz": {}, "bz2": {}, "7z": {}, "rar": {},
		"zst": {}, "lz": {}, "lz4": {}, "lzma": {},
	}
)

type Entry struct {
	Name         string
	Path         string
	Extension    string
	Mode         fs.FileMode
	Size         int64
	ModifiedAt   time.Time
	CreatedAt    time.Time
	CreatedKnown bool
	IsDir        bool
	IsParent     bool
	IsHidden     bool
	DirSizeKnown bool
}

func (e Entry) DisplayName() string {
	if e.IsParent {
		return ".."
	}
	if e.IsDir {
		return e.Name + "/"
	}
	return e.Name
}

func (e Entry) IsFile() bool {
	return !e.IsDir && !e.IsParent
}

func (e Entry) MatchKey() string {
	return strings.ToLower(e.Name)
}

func (e Entry) IsExecutable() bool {
	return !e.IsDir && !e.IsParent && e.Mode&0o111 != 0
}

func (e Entry) Category() string {
	switch {
	case e.IsParent:
		return "parent"
	case e.IsDir:
		return "directory"
	case e.IsExecutable():
		return "executable"
	case hasExt(configExtensions, e.Extension):
		return "config"
	case hasExt(imageExtensions, e.Extension):
		return "image"
	case hasExt(textExtensions, e.Extension):
		return "text"
	case hasExt(archiveExtensions, e.Extension):
		return "archive"
	case hasExt(textFilenames, strings.ToLower(e.Name)):
		return "text"
	default:
		return "binary"
	}
}

func ext(name string) string {
	value := strings.TrimPrefix(filepath.Ext(name), ".")
	return strings.ToLower(value)
}

func hasExt(set map[string]struct{}, ext string) bool {
	_, ok := set[strings.ToLower(ext)]
	return ok
}
