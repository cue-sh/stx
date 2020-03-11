package cmd

import (
	"io/ioutil"
	"os"
	"regexp"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"github.com/TangoGroup/stx/stx"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	"github.com/spf13/cobra"
)

// exeCmd represents the exe command
var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "DIFF against CloudFormation for the evaluted leaves.",
	Long:  `Yada yada yada.`,
	Run: func(cmd *cobra.Command, args []string) {
		defer log.Flush()

		stx.EnsureVaultSession(config)

		buildInstances := stx.GetBuildInstances(args, "cfn")

		stx.Process(buildInstances, flags, log, func(buildInstance *build.Instance, cueInstance *cue.Instance, cueValue cue.Value) {
			stacks, stacksErr := stx.GetStacks(cueValue, flags)
			if stacksErr != nil {
				log.Error(stacksErr)
			}

			if stacks == nil {
				return
			}

			for stackName, stack := range stacks {
				fileName, saveErr := saveStackAsYml(stackName, stack, buildInstance, cueValue)
				if saveErr != nil {
					log.Error(saveErr)
				}

				// get a session and cloudformation service client
				session := stx.GetSession(stack.Profile)
				cfn := cloudformation.New(session, aws.NewConfig().WithRegion(stack.Region))

				// read template from disk
				templateFileBytes, _ := ioutil.ReadFile(fileName)
				templateBody := string(templateFileBytes)

				// look to see if stack exists
				describeStacksInput := cloudformation.DescribeStacksInput{StackName: &stackName}
				describeStacksOutput, describeStacksErr := cfn.DescribeStacks(&describeStacksInput)

				if describeStacksErr != nil {
					log.Debugf("DESC STAX:\n%+v\n", describeStacksOutput)
					log.Error(describeStacksErr)
					continue
				}

				diff(cfn, stackName, templateBody)
			}

		})
	},
}

func diff(cfn *cloudformation.CloudFormation, stackName, templateBody string) {
	existingTemplate, err := cfn.GetTemplate(&cloudformation.GetTemplateInput{
		StackName: &stackName,
	})
	if err != nil {
		log.Error("Error getting template for stack", stackName)
	} else {
		r, _ := regexp.Compile("!(Base64|Cidr|FindInMap|GetAtt|GetAZs|ImportValue|Join|Select|Split|Sub|Transform|Ref|And|Equals|If|Not|Or)")
		if r.MatchString(aws.StringValue(existingTemplate.TemplateBody)) {
			log.Warn("The existing stack uses short intrinsic functions, unable to create diff: " + stackName)
		} else {
			existingDoc, _ := ytbx.LoadDocuments([]byte(aws.StringValue(existingTemplate.TemplateBody)))
			doc, _ := ytbx.LoadDocuments([]byte(templateBody))
			report, err := dyff.CompareInputFiles(
				ytbx.InputFile{Documents: existingDoc},
				ytbx.InputFile{Documents: doc},
			)
			if err != nil {
				log.Error("Error creating template diff for stack: " + stackName)
			} else {
				if len(report.Diffs) > 0 {
					reportWriter := &dyff.HumanReport{
						Report:     report,
						ShowBanner: false,
					}
					reportWriter.WriteReport(os.Stdout)
				}
			}
		}
	}
}

func init() {
	rootCmd.AddCommand(diffCmd)

	// TODO add a flag to watch events
}
