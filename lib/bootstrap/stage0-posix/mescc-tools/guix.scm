;;; guix.scm -- Guix package definition
;;; mescc-tools
;;; Copyright © 2017 Jan Nieuwenhuizen <janneke@gnu.org>
;;; Copyright 2016 Jeremiah Orians

;;; Also borrowing code from:
;;; guile-sdl2 --- FFI bindings for SDL2
;;; Copyright © 2015 David Thompson <davet@gnu.org>

;;;
;;; guix.scm: This file is part of mescc-tools.
;;;
;;; mescc-tools is free software; you can redistribute it and/or modify it
;;; under the terms of the GNU General Public License as published by
;;; the Free Software Foundation; either version 3 of the License, or (at
;;; your option) any later version.
;;;
;;; mescc-tools is distributed in the hope that it will be useful, but
;;; WITHOUT ANY WARRANTY; without even the implied warranty of
;;; MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
;;; GNU General Public License for more details.
;;;
;;; You should have received a copy of the GNU General Public License
;;; along with mescc-tools.  If not, see <http://www.gnu.org/licenses/>.

;;; Commentary:
;; GNU Guix development package.  To build and install, run:
;;   guix package -f guix.scm
;;
;; To build it, but not install it, run:
;;   guix build -f guix.scm
;;
;; To use as the basis for a development environment, run:
;;   guix environment -l guix.scm
;;
;;; Code:

(use-modules (srfi srfi-1)
             (srfi srfi-26)
             (ice-9 match)
             (ice-9 popen)
             (ice-9 rdelim)
             (gnu packages)
             (gnu packages gcc)
             (gnu packages base)
             ((guix build utils) #:select (with-directory-excursion))
             (guix build-system gnu)
             (guix gexp)
             (guix git-download)
             (guix licenses)
             (guix packages))

(define %source-dir (dirname (current-filename)))

(define git-file?
  (let* ((pipe (with-directory-excursion %source-dir
                 (open-pipe* OPEN_READ "git" "ls-files")))
         (files (let loop ((lines '()))
                  (match (read-line pipe)
                    ((? eof-object?)
                     (reverse lines))
                    (line
                     (loop (cons line lines))))))
         (status (close-pipe pipe)))
    (lambda (file stat)
      (match (stat:type stat)
        ('directory #t)
        ((or 'regular 'symlink)
         (any (cut string-suffix? <> file) files))
        (_ #f)))))


(define-public mescc-tools.git
  (package
     (name "mescc-tools.git")
     (build-system gnu-build-system)
     (inputs `(("which", which) ("coreutils", coreutils)))
     (arguments
      `(#:make-flags (list (string-append "PREFIX=" (assoc-ref %outputs "out")))
        #:test-target "test"
        #:phases
        (modify-phases %standard-phases
          (delete 'configure))))
     (synopsis "tools for the full source bootstrapping process")
     (description
      "Mescc-tools is a collection of tools for use in full source bootstrapping process.
Currently consists of the M0 macro assembler and the hex2 linker.")
     (home-page "https://github.com/oriansj/mescc-tools")
     (license gpl3+)
     (version (string-append "HEAD-" (string-take (read-string (open-pipe "git show HEAD | head -1 | cut -d ' ' -f 2" OPEN_READ)) 7)))
     (source (local-file %source-dir #:recursive? #t #:select? git-file?))))

;; Return it here so `guix build/environment/package' can consume it directly.
mescc-tools.git
