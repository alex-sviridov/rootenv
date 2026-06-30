/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("loadLabImageRef", func() {
	var dir string

	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "lab-images-test")
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Setenv("LAB_IMAGES_DIR", dir)).To(Succeed())
	})

	AfterEach(func() {
		Expect(os.Unsetenv("LAB_IMAGES_DIR")).To(Succeed())
		Expect(os.RemoveAll(dir)).To(Succeed())
	})

	It("returns the trimmed contents of the file matching the image name", func() {
		Expect(os.WriteFile(filepath.Join(dir, "ubuntu-sshd"), []byte("ubuntu-sshd:abc123\n"), 0644)).To(Succeed())

		ref, err := loadLabImageRef("ubuntu-sshd")
		Expect(err).NotTo(HaveOccurred())
		Expect(ref).To(Equal("ubuntu-sshd:abc123"))
	})

	It("falls back to the literal image name when no file matches", func() {
		ref, err := loadLabImageRef("ubuntu")
		Expect(err).NotTo(HaveOccurred())
		Expect(ref).To(Equal("ubuntu"))
	})

	It("falls back to the literal image name when LAB_IMAGES_DIR is unset and no default file exists", func() {
		Expect(os.Unsetenv("LAB_IMAGES_DIR")).To(Succeed())
		ref, err := loadLabImageRef("ubuntu")
		Expect(err).NotTo(HaveOccurred())
		Expect(ref).To(Equal("ubuntu"))
	})

	It("returns an error when the lab-images directory exists but the file can't be read for another reason", func() {
		Expect(os.WriteFile(filepath.Join(dir, "ubuntu-sshd"), []byte("ubuntu-sshd:abc123"), 0644)).To(Succeed())
		Expect(os.Chmod(filepath.Join(dir, "ubuntu-sshd"), 0000)).To(Succeed())

		_, err := loadLabImageRef("ubuntu-sshd")
		Expect(err).To(MatchError(ContainSubstring("ubuntu-sshd")))
	})
})
