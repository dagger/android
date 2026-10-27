package com.example;

import static org.junit.jupiter.api.Assertions.assertTrue;
import java.util.stream.Stream;
import org.junit.jupiter.api.DynamicTest;
import org.junit.jupiter.api.TestFactory;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

public class CasesTest {
    @ParameterizedTest
    @ValueSource(ints = {1, 2})
    void positiveNumbers(int value) {
        assertTrue(value > 0);
    }

    @TestFactory
    Stream<DynamicTest> dynamicCases() {
        return Stream.of(DynamicTest.dynamicTest("generated case", () -> assertTrue(true)));
    }
}
