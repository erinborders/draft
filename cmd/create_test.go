package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/Azure/draft/pkg/config"
	"github.com/Azure/draft/pkg/languages"
	"github.com/Azure/draft/pkg/linguist"
	"github.com/Azure/draft/pkg/reporeader"
	"github.com/Azure/draft/pkg/templatewriter/writers"
	"github.com/Azure/draft/template"
)

func TestRun(t *testing.T) {
	testCreateConfig := CreateConfig{LanguageVariables: []UserInputs{{Name: "PORT", Value: "8080"}}, DeployVariables: []UserInputs{{Name: "PORT", Value: "8080"}, {Name: "APPNAME", Value: "testingCreateCommand"}}}
	flagVariablesMap = map[string]string{"PORT": "8080", "APPNAME": "testingCreateCommand", "VERSION": "1.18", "SERVICEPORT": "8080", "NAMESPACE": "testNamespace", "IMAGENAME": "testImage", "IMAGETAG": "latest"}
	mockCC := createCmd{dest: "./..", createConfig: &testCreateConfig, templateWriter: &writers.LocalFSWriter{}}
	deployTypes := []string{"helm", "kustomize", "manifests"}
	oldDockerfile, _ := ioutil.ReadFile("./../Dockerfile")
	oldDockerignore, _ := ioutil.ReadFile("./../.dockerignore")

	detectedLang, lowerLang, err := mockCC.mockDetectLanguage()

	assert.False(t, detectedLang == nil)
	assert.False(t, lowerLang == "")
	assert.True(t, err == nil)

	err = mockCC.generateDockerfile(detectedLang, lowerLang)
	assert.True(t, err == nil)

	//when language variables are passed in --variable flag
	mockCC.createConfig.LanguageVariables = nil
	mockCC.lang = "go"
	detectedLang, lowerLang, err = mockCC.mockDetectLanguage()
	assert.False(t, detectedLang == nil)
	assert.False(t, lowerLang == "")
	assert.True(t, err == nil)
	err = mockCC.generateDockerfile(detectedLang, lowerLang)
	assert.True(t, err == nil)

	//Write back old Dockerfile
	err = ioutil.WriteFile("./../Dockerfile", oldDockerfile, 0644)
	if err != nil {
		t.Error(err)
	}

	err = ioutil.WriteFile("./../.dockerignore", oldDockerignore, 0644)
	if err != nil {
		t.Error(err)
	}

	for _, deployType := range deployTypes {
		//deployment variables passed through --variable flag
		mockCC.deployType = deployType
		err = mockCC.createDeployment()
		assert.True(t, err == nil)
		//check if deployment files have been created
		err, deploymentFiles := getAllDeploymentFiles(path.Join("../template/deployments", mockCC.deployType))
		assert.Nil(t, err)
		for _, fileName := range deploymentFiles {
			_, err = os.Stat(fileName)
			assert.True(t, err == nil)
		}

		os.RemoveAll("./../charts")
		os.RemoveAll("./../base")
		os.RemoveAll("./../overlays")
		os.RemoveAll("./../manifests")

		//deployment variables passed through createConfig
		mockCC.createConfig.DeployType = deployType
		err = mockCC.createDeployment()
		assert.True(t, err == nil)
		//check if deployment files have been created
		err, deploymentFiles = getAllDeploymentFiles(path.Join("../template/deployments", mockCC.createConfig.DeployType))
		assert.Nil(t, err)
		for _, fileName := range deploymentFiles {
			_, err = os.Stat(fileName)
			assert.True(t, err == nil)
		}
		mockCC.createConfig.DeployType = ""

		os.RemoveAll("./../charts")
		os.RemoveAll("./../base")
		os.RemoveAll("./../overlays")
		os.RemoveAll("./../manifests")
	}
}

func TestRunCreateDockerfileWithRepoReader(t *testing.T) {

	testRepoReader := &reporeader.TestRepoReader{Files: map[string][]byte{
		"foo.py":  []byte("print('Hello World')"),
		"main.py": []byte("print('Hello World')"),
	}}

	testCreateConfig := CreateConfig{LanguageType: "python", LanguageVariables: []UserInputs{{Name: "PORT", Value: "8080"}}}
	mockCC := createCmd{createConfig: &testCreateConfig, repoReader: testRepoReader, templateWriter: &writers.LocalFSWriter{}}

	detectedLang, lowerLang, err := mockCC.mockDetectLanguage()
	assert.False(t, detectedLang == nil)
	assert.True(t, lowerLang == "python")
	assert.Nil(t, err)

	err = mockCC.generateDockerfile(detectedLang, lowerLang)
	assert.True(t, err == nil)

	dockerFileContent, err := ioutil.ReadFile("Dockerfile")
	if err != nil {
		t.Error(err)
	}
	assert.Contains(t, string(dockerFileContent), "CMD [\"main.py\"]")

	err = os.Remove("Dockerfile")
	if err != nil {
		t.Error(err)
	}
	err = os.RemoveAll(".dockerignore")
	if err != nil {
		t.Error(err)
	}
}

func TestInitConfig(t *testing.T) {
	mockCC := &createCmd{}
	mockCC.createConfig = &CreateConfig{}
	mockCC.dest = "./.."
	mockCC.createConfigPath = "./../test/templates/config.yaml"

	err := mockCC.initConfig()
	assert.True(t, err == nil)
	assert.True(t, mockCC.createConfig != nil)
}

func TestValidateConfigInputsToPromptsPass(t *testing.T) {
	required := []config.BuilderVar{
		{Name: "REQUIRED_PROVIDED"},
		{Name: "REQUIRED_DEFAULTED"},
	}
	provided := []UserInputs{
		{Name: "REQUIRED_PROVIDED", Value: "PROVIDED_VALUE"},
	}
	defaults := []config.BuilderVarDefault{
		{Name: "REQUIRED_DEFAULTED", Value: "DEFAULT_VALUE"},
	}

	vars, err := validateConfigInputsToPrompts(required, provided, defaults)
	assert.True(t, err == nil)
	assert.Equal(t, vars["REQUIRED_DEFAULTED"], "DEFAULT_VALUE")
}

func TestValidateConfigInputsToPromptsMissing(t *testing.T) {
	required := []config.BuilderVar{
		{Name: "REQUIRED_PROVIDED"},
		{Name: "REQUIRED_MISSING"},
	}
	provided := []UserInputs{
		{Name: "REQUIRED_PROVIDED"},
	}
	defaults := []config.BuilderVarDefault{}

	_, err := validateConfigInputsToPrompts(required, provided, defaults)
	assert.NotNil(t, err)
}

func (mcc *createCmd) mockDetectLanguage() (*config.DraftConfig, string, error) {
	hasGo := false
	hasGoMod := false
	var langs []*linguist.Language
	var err error

	if mcc.createConfig.LanguageType == "" {
		if mcc.lang != "" {
			mcc.createConfig.LanguageType = mcc.lang
		} else {
			langs, err = linguist.ProcessDir(mcc.dest)
			log.Debugf("linguist.ProcessDir(%v) result:\n\nError: %v", mcc.dest, err)
			if err != nil {
				return nil, "", fmt.Errorf("there was an error detecting the language: %s", err)
			}

			for _, lang := range langs {
				log.Debugf("%s:\t%f (%s)", lang.Language, lang.Percent, lang.Color)
			}

			log.Debugf("detected %d langs", len(langs))

			if len(langs) == 0 {
				return nil, "", ErrNoLanguageDetected
			}
		}
	}

	mcc.supportedLangs = languages.CreateLanguagesFromEmbedFS(template.Dockerfiles, mcc.dest)

	if mcc.createConfig.LanguageType != "" {
		log.Debug("using configuration language")
		lowerLang := strings.ToLower(mcc.createConfig.LanguageType)
		langConfig := mcc.supportedLangs.GetConfig(lowerLang)
		if langConfig == nil {
			return nil, "", ErrNoLanguageDetected
		}

		return langConfig, lowerLang, nil
	}

	for _, lang := range langs {
		detectedLang := linguist.Alias(lang)
		log.Infof("--> Draft detected %s (%f%%)\n", detectedLang.Language, detectedLang.Percent)
		lowerLang := strings.ToLower(detectedLang.Language)

		if mcc.supportedLangs.ContainsLanguage(lowerLang) {
			if lowerLang == "go" && hasGo && hasGoMod {
				log.Debug("detected go and go module")
				lowerLang = "gomodule"
			}

			langConfig := mcc.supportedLangs.GetConfig(lowerLang)
			return langConfig, lowerLang, nil
		}
		log.Infof("--> Could not find a pack for %s. Trying to find the next likely language match...\n", detectedLang.Language)
	}
	return nil, "", ErrNoLanguageDetected
}

func TestDefaultValues(t *testing.T) {
	assert.Equal(t, emptyDefaultFlagValue, "")
	assert.Equal(t, currentDirDefaultFlagValue, ".")
}

func getAllDeploymentFiles(src string) (error, []string) {
	deploymentFiles := []string{}
	err := filepath.Walk(src,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			filePath := strings.ReplaceAll(path, src, "./..")
			if info.Name() != "draft.yaml" {
				deploymentFiles = append(deploymentFiles, filePath)
			}
			return nil
		})
	return err, deploymentFiles
}
