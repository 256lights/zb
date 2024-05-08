;;; mescc-tools.scm -- Guix package definition
;;; Copyright © 2017 Jan Nieuwenhuizen <janneke@gnu.org>
;;; Copyright 2016 Jeremiah Orians
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

(use-modules (ice-9 match)
             (gnu packages)
             (gnu packages gcc)
             (gnu packages base)
             (guix build-system gnu)
             (guix download)
             (guix licenses)
             (guix packages))

(define-public mescc-tools
    (package
      (name "mescc-tools")
      (version "1.0.1")
      (inputs `(("which", which) ("coreutils", coreutils)))
      (source (origin
                (method url-fetch)
                (uri (string-append "http://git.savannah.nongnu.org/cgit/mescc-tools.git/snapshot/mescc-tools-Release_" version ".tar.gz"))
                (sha256
                 (base32 "1wqj70h4rrxl1d1aqpxhy47964r5dilvll6gvqv75y9qk6pwx5is"))))
      (build-system gnu-build-system)
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
      (license gpl3+)))

;; Return it here so `guix build/environment/package' can consume it directly.
mescc-tools
