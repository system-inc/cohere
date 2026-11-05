package react

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// TestNoUnstableNestedComponentsStaysSilent holds every passing case upstream ships,
// imported verbatim from
// eslint-plugin-react/tests/lib/rules/no-unstable-nested-components.js at 7.37.5.
//
// Several carry an upstream comment marking them a known false negative rather than a
// deliberate exemption. Those are reproduced as silent because that is what upstream does,
// and the comment is preserved at the case so the next reader does not read the silence as a
// judgment we made.
func TestNoUnstableNestedComponentsStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name    string
		source  string
		options *NoUnstableNestedComponentsOptions
	}{
		{
			name: "upstream valid 0",
			source: `
        function ParentComponent() {
          return (
            <div>
              <OutsideDefinedFunctionComponent />
            </div>
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 1",
			source: `
        function ParentComponent() {
          return React.createElement(
            "div",
            null,
            React.createElement(OutsideDefinedFunctionComponent, null)
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 2",
			source: `
        function ParentComponent() {
          return (
            <SomeComponent
              footer={<OutsideDefinedComponent />}
              header={<div />}
              />
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 3",
			source: `
        function ParentComponent() {
          return React.createElement(SomeComponent, {
            footer: React.createElement(OutsideDefinedComponent, null),
            header: React.createElement("div", null)
          });
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 4",
			source: `
        function ParentComponent() {
          const MemoizedNestedComponent = React.useCallback(() => <div />, []);

          return (
            <div>
              <MemoizedNestedComponent />
            </div>
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 5",
			source: `
        function ParentComponent() {
          const MemoizedNestedComponent = React.useCallback(
            () => React.createElement("div", null),
            []
          );

          return React.createElement(
            "div",
            null,
            React.createElement(MemoizedNestedComponent, null)
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 6",
			source: `
        function ParentComponent() {
          const MemoizedNestedFunctionComponent = React.useCallback(
            function () {
              return <div />;
            },
            []
          );

          return (
            <div>
              <MemoizedNestedFunctionComponent />
            </div>
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 7",
			source: `
        function ParentComponent() {
          const MemoizedNestedFunctionComponent = React.useCallback(
            function () {
              return React.createElement("div", null);
            },
            []
          );

          return React.createElement(
            "div",
            null,
            React.createElement(MemoizedNestedFunctionComponent, null)
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 8",
			source: `
        function ParentComponent(props) {
          // Should not interfere handler declarations
          function onClick(event) {
            props.onClick(event.target.value);
          }

          const onKeyPress = () => null;

          function getOnHover() {
            return function onHover(event) {
              props.onHover(event.target);
            }
          }

          return (
            <div>
              <button
                onClick={onClick}
                onKeyPress={onKeyPress}
                onHover={getOnHover()}

                // These should not be considered as components
                maybeComponentOrHandlerNull={() => null}
                maybeComponentOrHandlerUndefined={() => undefined}
                maybeComponentOrHandlerBlank={() => ''}
                maybeComponentOrHandlerString={() => 'hello-world'}
                maybeComponentOrHandlerNumber={() => 42}
                maybeComponentOrHandlerArray={() => []}
                maybeComponentOrHandlerObject={() => {}} />
            </div>
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 9",
			source: `
        function ParentComponent() {
          function getComponent() {
            return <div />;
          }

          return (
            <div>
              {getComponent()}
            </div>
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 10",
			source: `
        function ParentComponent() {
          function getComponent() {
            return React.createElement("div", null);
          }

          return React.createElement("div", null, getComponent());
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 11",
			source: `
        function ParentComponent() {
            return (
              <RenderPropComponent>
                {() => <div />}
              </RenderPropComponent>
            );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 12",
			source: `
        function ParentComponent() {
            return (
              <RenderPropComponent children={() => <div />} />
            );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 13",
			source: `
        function ParentComponent() {
          return (
            <ComplexRenderPropComponent
              listRenderer={data.map((items, index) => (
                <ul>
                  {items[index].map((item) =>
                    <li>
                      {item}
                    </li>
                  )}
                </ul>
              ))
              }
            />
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 14",
			source: `
        function ParentComponent() {
          return React.createElement(
              RenderPropComponent,
              null,
              () => React.createElement("div", null)
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 15",
			source: `
        function ParentComponent(props) {
          return (
            <ul>
              {props.items.map(item => (
                <li key={item.id}>
                  {item.name}
                </li>
              ))}
            </ul>
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 16",
			source: `
        function ParentComponent(props) {
          return (
            <List items={props.items.map(item => {
              return (
                <li key={item.id}>
                  {item.name}
                </li>
              );
            })}
            />
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 17",
			source: `
        function ParentComponent(props) {
          return React.createElement(
            "ul",
            null,
            props.items.map(() =>
              React.createElement(
                "li",
                { key: item.id },
                item.name
              )
            )
          )
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 18",
			source: `
        function ParentComponent(props) {
          return (
            <ul>
              {props.items.map(function Item(item) {
                return (
                  <li key={item.id}>
                    {item.name}
                  </li>
                );
              })}
            </ul>
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 19",
			source: `
        function ParentComponent(props) {
          return React.createElement(
            "ul",
            null,
            props.items.map(function Item() {
              return React.createElement(
                "li",
                { key: item.id },
                item.name
              );
            })
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 20",
			source: `
        function createTestComponent(props) {
          return (
            <div />
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 21",
			source: `
        function createTestComponent(props) {
          return React.createElement("div", null);
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 22",
			source: `
        function ParentComponent() {
          return (
            <ComponentWithProps footer={() => <div />} />
          );
        }
      `,
			options: &NoUnstableNestedComponentsOptions{AllowAsProps: true},
		},
		{
			name: "upstream valid 23",
			source: `
        function ParentComponent() {
          return React.createElement(ComponentWithProps, {
            footer: () => React.createElement("div", null)
          });
        }
      `,
			options: &NoUnstableNestedComponentsOptions{AllowAsProps: true},
		},
		{
			name: "upstream valid 24",
			source: `
        function ParentComponent() {
          return (
            <SomeComponent item={{ children: () => <div /> }} />
          )
        }
      `,
			options: &NoUnstableNestedComponentsOptions{AllowAsProps: true},
		},
		{
			name: "upstream valid 25",
			source: `
      function ParentComponent() {
        return (
          <SomeComponent>
            {
              thing.match({
                renderLoading: () => <div />,
                renderSuccess: () => <div />,
                renderFailure: () => <div />,
              })
            }
          </SomeComponent>
        )
      }
      `,
			options: nil,
		},
		{
			name: "upstream valid 26",
			source: `
      function ParentComponent() {
        const thingElement = thing.match({
          renderLoading: () => <div />,
          renderSuccess: () => <div />,
          renderFailure: () => <div />,
        });
        return (
          <SomeComponent>
            {thingElement}
          </SomeComponent>
        )
      }
      `,
			options: nil,
		},
		{
			name: "upstream valid 27",
			source: `
      function ParentComponent() {
        return (
          <SomeComponent>
            {
              thing.match({
                loading: () => <div />,
                success: () => <div />,
                failure: () => <div />,
              })
            }
          </SomeComponent>
        )
      }
      `,
			options: &NoUnstableNestedComponentsOptions{AllowAsProps: true},
		},
		{
			name: "upstream valid 28",
			source: `
      function ParentComponent() {
        const thingElement = thing.match({
          loading: () => <div />,
          success: () => <div />,
          failure: () => <div />,
        });
        return (
          <SomeComponent>
            {thingElement}
          </SomeComponent>
        )
      }
      `,
			options: &NoUnstableNestedComponentsOptions{AllowAsProps: true},
		},
		{
			name: "upstream valid 29",
			source: `
        function ParentComponent() {
          return (
            <ComponentForProps renderFooter={() => <div />} />
          );
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 30",
			source: `
        function ParentComponent() {
          return React.createElement(ComponentForProps, {
            renderFooter: () => React.createElement("div", null)
          });
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 31",
			source: `
        function ParentComponent() {
          useEffect(() => {
            return () => null;
          });

          return <div />;
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 32",
			source: `
        function ParentComponent() {
          return (
            <SomeComponent renderers={{ Header: () => <div /> }} />
          )
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 33",
			source: `
        function ParentComponent() {
          return (
            <SomeComponent renderMenu={() => (
              <RenderPropComponent>
                {items.map(item => (
                  <li key={item}>{item}</li>
                ))}
              </RenderPropComponent>
            )} />
          )
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 34",
			source: `
        const ParentComponent = () => (
          <SomeComponent
            components={[
              <ul>
                {list.map(item => (
                  <li key={item}>{item}</li>
                ))}
              </ul>,
            ]}
          />
        );
     `,
			options: nil,
		},
		{
			name: "upstream valid 35",
			source: `
        function ParentComponent() {
          const rows = [
            {
              name: 'A',
              render: (props) => <Row {...props} />
            },
          ];

          return <Table rows={rows} />;
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 36",
			source: `
        function ParentComponent() {
          return <SomeComponent renderers={{ notComponent: () => null }} />;
        }
      `,
			options: nil,
		},
		{
			name: "upstream valid 37",
			source: `
        const ParentComponent = createReactClass({
          displayName: "ParentComponent",
          statics: {
            getSnapshotBeforeUpdate: function () {
              return null;
            },
          },
          render() {
            return <div />;
          },
        });
      `,
			options: nil,
		},
		{
			name: "upstream valid 38",
			source: `
        function ParentComponent() {
          const rows = [
            {
              name: 'A',
              notPrefixedWithRender: (props) => <Row {...props} />
            },
          ];

          return <Table rows={rows} />;
        }
      `,
			options: &NoUnstableNestedComponentsOptions{AllowAsProps: true},
		},
		{
			name: "upstream valid 39",
			source: `
        function ParentComponent() {
          return <Table
            rowRenderer={(rowData) => <Row data={data} />}
          />
        }
      `,
			options: &NoUnstableNestedComponentsOptions{PropNamePattern: "*Renderer"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			options := DefaultNoUnstableNestedComponentsOptions()
			if testCase.options != nil {
				options = *testCase.options
				if options.PropNamePattern == "" {
					options.PropNamePattern = "render*"
				}
			}
			result := rule_testing.RunTypedWithOptions(
				test,
				NoUnstableNestedComponents,
				"Component.tsx",
				testCase.source,
				options,
			)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// TestNoUnstableNestedComponentsFires holds every failing case upstream ships, imported
// verbatim, with the finding count upstream asserts for each.
func TestNoUnstableNestedComponentsFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name    string
		source  string
		count   int
		options *NoUnstableNestedComponentsOptions
	}{
		{
			name: "upstream invalid 0",
			source: `
        function ParentComponent() {
          function UnstableNestedFunctionComponent() {
            return <div />;
          }

          return (
            <div>
              <UnstableNestedFunctionComponent />
            </div>
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 1",
			source: `
        function ParentComponent() {
          function UnstableNestedFunctionComponent() {
            return React.createElement("div", null);
          }

          return React.createElement(
            "div",
            null,
            React.createElement(UnstableNestedFunctionComponent, null)
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 2",
			source: `
        function ParentComponent() {
          const UnstableNestedVariableComponent = () => {
            return <div />;
          }

          return (
            <div>
              <UnstableNestedVariableComponent />
            </div>
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 3",
			source: `
        function ParentComponent() {
          const UnstableNestedVariableComponent = () => {
            return React.createElement("div", null);
          }

          return React.createElement(
            "div",
            null,
            React.createElement(UnstableNestedVariableComponent, null)
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 4",
			source: `
        const ParentComponent = () => {
          function UnstableNestedFunctionComponent() {
            return <div />;
          }

          return (
            <div>
              <UnstableNestedFunctionComponent />
            </div>
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 5",
			source: `
        const ParentComponent = () => {
          function UnstableNestedFunctionComponent() {
            return React.createElement("div", null);
          }

          return React.createElement(
            "div",
            null,
            React.createElement(UnstableNestedFunctionComponent, null)
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 6",
			source: `
        export default () => {
          function UnstableNestedFunctionComponent() {
            return <div />;
          }

          return (
            <div>
              <UnstableNestedFunctionComponent />
            </div>
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 7",
			source: `
        export default () => {
          function UnstableNestedFunctionComponent() {
            return React.createElement("div", null);
          }

          return React.createElement(
            "div",
            null,
            React.createElement(UnstableNestedFunctionComponent, null)
          );
        };
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 8",
			source: `
        const ParentComponent = () => {
          const UnstableNestedVariableComponent = () => {
            return <div />;
          }

          return (
            <div>
              <UnstableNestedVariableComponent />
            </div>
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 9",
			source: `
        const ParentComponent = () => {
          const UnstableNestedVariableComponent = () => {
            return React.createElement("div", null);
          }

          return React.createElement(
            "div",
            null,
            React.createElement(UnstableNestedVariableComponent, null)
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 10",
			source: `
        function ParentComponent() {
          class UnstableNestedClassComponent extends React.Component {
            render() {
              return <div />;
            }
          };

          return (
            <div>
              <UnstableNestedClassComponent />
            </div>
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 11",
			source: `
        function ParentComponent() {
          class UnstableNestedClassComponent extends React.Component {
            render() {
              return React.createElement("div", null);
            }
          }

          return React.createElement(
            "div",
            null,
            React.createElement(UnstableNestedClassComponent, null)
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 12",
			source: `
        class ParentComponent extends React.Component {
          render() {
            class UnstableNestedClassComponent extends React.Component {
              render() {
                return <div />;
              }
            };

            return (
              <div>
                <UnstableNestedClassComponent />
              </div>
            );
          }
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 13",
			source: `
        class ParentComponent extends React.Component {
          render() {
            class UnstableNestedClassComponent extends React.Component {
              render() {
                return React.createElement("div", null);
              }
            }

            return React.createElement(
              "div",
              null,
              React.createElement(UnstableNestedClassComponent, null)
            );
          }
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 14",
			source: `
        class ParentComponent extends React.Component {
          render() {
            function UnstableNestedFunctionComponent() {
              return <div />;
            }

            return (
              <div>
                <UnstableNestedFunctionComponent />
              </div>
            );
          }
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 15",
			source: `
        class ParentComponent extends React.Component {
          render() {
            function UnstableNestedClassComponent() {
              return React.createElement("div", null);
            }

            return React.createElement(
              "div",
              null,
              React.createElement(UnstableNestedClassComponent, null)
            );
          }
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 16",
			source: `
        class ParentComponent extends React.Component {
          render() {
            const UnstableNestedVariableComponent = () => {
              return <div />;
            }

            return (
              <div>
                <UnstableNestedVariableComponent />
              </div>
            );
          }
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 17",
			source: `
        class ParentComponent extends React.Component {
          render() {
            const UnstableNestedClassComponent = () => {
              return React.createElement("div", null);
            }

            return React.createElement(
              "div",
              null,
              React.createElement(UnstableNestedClassComponent, null)
            );
          }
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 18",
			source: `
        function ParentComponent() {
          function getComponent() {
            function NestedUnstableFunctionComponent() {
              return <div />;
            };

            return <NestedUnstableFunctionComponent />;
          }

          return (
            <div>
              {getComponent()}
            </div>
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 19",
			source: `
        function ParentComponent() {
          function getComponent() {
            function NestedUnstableFunctionComponent() {
              return React.createElement("div", null);
            }

            return React.createElement(NestedUnstableFunctionComponent, null);
          }

          return React.createElement("div", null, getComponent());
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 20",
			source: `
        function ComponentWithProps(props) {
          return <div />;
        }

        function ParentComponent() {
          return (
            <ComponentWithProps
              footer={
                function SomeFooter() {
                  return <div />;
                }
              } />
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 21",
			source: `
        function ComponentWithProps(props) {
          return React.createElement("div", null);
        }

        function ParentComponent() {
          return React.createElement(ComponentWithProps, {
            footer: function SomeFooter() {
              return React.createElement("div", null);
            }
          });
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 22",
			source: `
        function ComponentWithProps(props) {
          return <div />;
        }

        function ParentComponent() {
            return (
              <ComponentWithProps footer={() => <div />} />
            );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 23",
			source: `
        function ComponentWithProps(props) {
          return React.createElement("div", null);
        }

        function ParentComponent() {
          return React.createElement(ComponentWithProps, {
            footer: () => React.createElement("div", null)
          });
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 24",
			source: `
        function ParentComponent() {
            return (
              <RenderPropComponent>
                {() => {
                  function UnstableNestedComponent() {
                    return <div />;
                  }

                  return (
                    <div>
                      <UnstableNestedComponent />
                    </div>
                  );
                }}
              </RenderPropComponent>
            );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 25",
			source: `
        function RenderPropComponent(props) {
          return props.render({});
        }

        function ParentComponent() {
          return React.createElement(
            RenderPropComponent,
            null,
            () => {
              function UnstableNestedComponent() {
                return React.createElement("div", null);
              }

              return React.createElement(
                "div",
                null,
                React.createElement(UnstableNestedComponent, null)
              );
            }
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 26",
			source: `
        function ComponentForProps(props) {
          return <div />;
        }

        function ParentComponent() {
          return (
            <ComponentForProps notPrefixedWithRender={() => <div />} />
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 27",
			source: `
        function ComponentForProps(props) {
          return React.createElement("div", null);
        }

        function ParentComponent() {
          return React.createElement(ComponentForProps, {
            notPrefixedWithRender: () => React.createElement("div", null)
          });
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 28",
			source: `
        function ParentComponent() {
          return (
            <ComponentForProps someMap={{ Header: () => <div /> }} />
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 29",
			source: `
        class ParentComponent extends React.Component {
          render() {
            const List = (props) => {
              const items = props.items
                .map((item) => (
                  <li key={item.key}>
                    <span>{item.name}</span>
                  </li>
                ));

              return <ul>{items}</ul>;
            };

            return <List {...this.props} />;
          }
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 30",
			source: `
      function ParentComponent() {
        return (
          <SomeComponent>
            {
              thing.match({
                loading: () => <div />,
                success: () => <div />,
                failure: () => <div />,
              })
            }
          </SomeComponent>
        )
      }
      `,
			count:   3,
			options: nil,
		},
		{
			name: "upstream invalid 31",
			source: `
      function ParentComponent() {
        const thingElement = thing.match({
          loading: () => <div />,
          success: () => <div />,
          failure: () => <div />,
        });
        return (
          <SomeComponent>
            {thingElement}
          </SomeComponent>
        )
      }
      `,
			count:   3,
			options: nil,
		},
		{
			name: "upstream invalid 32",
			source: `
      function ParentComponent() {
        const rows = [
          {
            name: 'A',
            notPrefixedWithRender: (props) => <Row {...props} />
          },
        ];

        return <Table rows={rows} />;
      }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 33",
			source: `
        function ParentComponent() {
          const UnstableNestedComponent = React.memo(() => {
            return <div />;
          });

          return (
            <div>
              <UnstableNestedComponent />
            </div>
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 34",
			source: `
        function ParentComponent() {
          const UnstableNestedComponent = React.memo(
            () => React.createElement("div", null),
          );

          return React.createElement(
            "div",
            null,
            React.createElement(UnstableNestedComponent, null)
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 35",
			source: `
        function ParentComponent() {
          const UnstableNestedComponent = React.memo(
            function () {
              return <div />;
            }
          );

          return (
            <div>
              <UnstableNestedComponent />
            </div>
          );
        }
      `,
			count:   1,
			options: nil,
		},
		{
			name: "upstream invalid 36",
			source: `
        function ParentComponent() {
          const UnstableNestedComponent = React.memo(
            function () {
              return React.createElement("div", null);
            }
          );

          return React.createElement(
            "div",
            null,
            React.createElement(UnstableNestedComponent, null)
          );
        }
      `,
			count:   1,
			options: nil,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			options := DefaultNoUnstableNestedComponentsOptions()
			if testCase.options != nil {
				options = *testCase.options
				if options.PropNamePattern == "" {
					options.PropNamePattern = "render*"
				}
			}
			result := rule_testing.RunTypedWithOptions(
				test,
				NoUnstableNestedComponents,
				"Component.tsx",
				testCase.source,
				options,
			)
			expected := make([]string, testCase.count)
			for index := range expected {
				expected[index] = "unstableNestedComponent"
			}
			rule_testing.ExpectFindings(test, result, expected...)
		})
	}
}
